/*
 * Copyright (c) 2019 Huawei Technologies Co., Ltd.
 * A-Tune is licensed under the Mulan PSL v2.
 * You can use this software according to the terms and conditions of the Mulan PSL v2.
 * You may obtain a copy of Mulan PSL v2 at:
 *     http://license.coscl.org.cn/MulanPSL2
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR
 * PURPOSE.
 * See the Mulan PSL v2 for more details.
 * Create: 2019-10-29
 */

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	PB "gitee.com/openeuler/A-Tune/api/profile"
	_ "gitee.com/openeuler/A-Tune/common/checker"
	"gitee.com/openeuler/A-Tune/common/config"
	"gitee.com/openeuler/A-Tune/common/http"
	"gitee.com/openeuler/A-Tune/common/log"
	"gitee.com/openeuler/A-Tune/common/models"
	"gitee.com/openeuler/A-Tune/common/profile"
	"gitee.com/openeuler/A-Tune/common/registry"
	"gitee.com/openeuler/A-Tune/common/schedule"
	SVC "gitee.com/openeuler/A-Tune/common/service"
	"gitee.com/openeuler/A-Tune/common/sqlstore"
	"gitee.com/openeuler/A-Tune/common/tuning"
	"gitee.com/openeuler/A-Tune/common/utils"
	"io"
	"io/ioutil"
	"mime/multipart"
	HTTP "net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-ini/ini"
	"github.com/mitchellh/mapstructure"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
)

// Monitor : the body send to monitor service
type Monitor struct {
	Module  string `json:"module"`
	Purpose string `json:"purpose"`
	Field   string `json:"field"`
}

// CollectorPost : the body send to collection service
type CollectorPost struct {
	Monitors  []Monitor `json:"monitors"`
	SampleNum int       `json:"sample_num"`
	Pipe      string    `json:"pipe"`
	File      string    `json:"file"`
	DataType  string    `json:"data_type"`
}

// RespCollectorPost : the response of collection servie
type RespCollectorPost struct {
	Path string                 `json:"path"`
	Data map[string]interface{} `json:"data"`
}

// ClassifyPostBody : the body send to classify service
type ClassifyPostBody struct {
	Data      string `json:"data"`
	ModelPath string `json:"modelpath,omitempty"`
	Model     string `json:"model,omitempty"`
}

// RespClassify : the response of classify model
type RespClassify struct {
	ResourceLimit string  `json:"resource_limit"`
	WorkloadType  string  `json:"workload_type"`
	Percentage    float32 `json:"percentage"`
}

// ProfileServer : the type impletent the grpc server
type ProfileServer struct {
	utils.MutexLock
	ConfPath   string
	ScriptPath string
	Raw        *ini.File
}

func init() {
	svc := SVC.ProfileService{
		Name:    "opt.profile",
		Desc:    "opt profile module",
		NewInst: NewProfileServer,
	}
	if err := SVC.AddService(&svc); err != nil {
		fmt.Printf("Failed to load service project : %s\n", err)
		return
	}

	log.Info("load profile service successfully\n")
}

// NewProfileServer method new a instance of the grpc server
func NewProfileServer(ctx *cli.Context, opts ...interface{}) (interface{}, error) {
	defaultConfigFile := path.Join(config.DefaultConfPath, "atuned.cnf")

	exist, err := utils.PathExist(defaultConfigFile)
	if err != nil {
		return nil, err
	}
	if !exist {
		return nil, fmt.Errorf("could not find default config file")
	}

	cfg, err := ini.Load(defaultConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s, %v", defaultConfigFile, err)
	}

	return &ProfileServer{
		Raw: cfg,
	}, nil
}

// RegisterServer method register the grpc service
func (s *ProfileServer) RegisterServer(server *grpc.Server) error {
	PB.RegisterProfileMgrServer(server, s)
	return nil
}

// Healthy method, implement SvrService interface
func (s *ProfileServer) Healthy(opts ...interface{}) error {
	return nil
}

// Post method send POST to start analysis the workload type
func (p *ClassifyPostBody) Post() (*RespClassify, error) {
	url := config.GetURL(config.ClassificationURI)
	response, err := http.Post(url, p)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("online learning failed")
	}
	resBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	resPostIns := new(RespClassify)
	err = json.Unmarshal(resBody, resPostIns)
	if err != nil {
		return nil, err
	}

	return resPostIns, nil
}

// Post method send POST to start collection data
func (c *CollectorPost) Post() (*RespCollectorPost, error) {
	url := config.GetURL(config.CollectorURI)
	response, err := http.Post(url, c)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("collect data failed")
	}
	resBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	resPostIns := new(RespCollectorPost)
	err = json.Unmarshal(resBody, resPostIns)
	if err != nil {
		return nil, err
	}
	return resPostIns, nil
}

// Post method send POST to start transfer file
func Post(serviceType, paramName, path string) (string, error) {
	url := config.GetURL(config.TransferURI)
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Path Error")
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(paramName, filepath.Base(path))
	if err != nil {
		return "", fmt.Errorf("writer error")
	}
	_, err = io.Copy(part, file)
	extraParams := map[string]string{
		"service":  serviceType,
		"savepath": "/etc/atuned/" + serviceType + "/" + filepath.Base(path),
	}

	for key, val := range extraParams {
		_ = writer.WriteField(key, val)
	}
	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("writer close error")
	}

	request, err := HTTP.NewRequest("POST", url, body)
	if err != nil {
		return "", fmt.Errorf("newRequest failed")
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	client := &HTTP.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("do request error")
	} else {
		defer resp.Body.Close()

		body := &bytes.Buffer{}
		_, err := body.ReadFrom(resp.Body)
		if err != nil {
			return "", fmt.Errorf("body read form error")
		}
		res := body.String()
		res = res[1 : len(res)-2]
		return res, nil
	}
}

// Profile method set the workload type to effective manual
func (s *ProfileServer) Profile(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_ProfileServer) error {
	profileNamesStr := profileInfo.GetName()
	profileNames := strings.Split(profileNamesStr, ",")
	profile, ok := profile.Load(profileNames)

	if !ok {
		fmt.Println("Failure Load ", profileInfo.GetName())
		return fmt.Errorf("load profile %s Faild", profileInfo.GetName())
	}
	ch := make(chan *PB.AckCheck)
	ctx, cancel := context.WithCancel(context.Background())
	defer close(ch)
	defer cancel()

	go func(ctx context.Context) {
		for {
			select {
			case value := <-ch:
				_ = stream.Send(value)
			case <-ctx.Done():
				return
			}
		}
	}(ctx)

	if err := profile.RollbackActive(ch); err != nil {
		return err
	}

	return nil
}

// ListWorkload method list support workload
func (s *ProfileServer) ListWorkload(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_ListWorkloadServer) error {
	log.Debug("Begin to inquire all workloads\n")
	profileLogs, err := sqlstore.GetProfileLogs()
	if err != nil {
		return err
	}

	var activeName string
	if len(profileLogs) > 0 {
		activeName = profileLogs[0].ProfileID
	}

	if activeName != "" {
		classProfile := &sqlstore.GetClass{Class: activeName}
		err = sqlstore.GetClasses(classProfile)
		if err != nil {
			return fmt.Errorf("inquery workload type table faild %v", err)
		}
		if len(classProfile.Result) > 0 {
			activeName = classProfile.Result[0].ProfileType
		}
	}
	log.Debugf("active name is %s", activeName)

	err = filepath.Walk(config.DefaultProfilePath, func(absPath string, info os.FileInfo, err error) error {
		if info.Name() == "include" {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if !strings.HasSuffix(info.Name(), ".conf") {
				return nil
			}

			absFilename := absPath[len(config.DefaultProfilePath)+1:]
			filenameOnly := strings.TrimSuffix(strings.ReplaceAll(absFilename, "/", "-"),
				path.Ext(info.Name()))

			var active bool
			if filenameOnly == activeName {
				active = true
			}
			_ = stream.Send(&PB.ListMessage{
				ProfileNames: filenameOnly,
				Active:       strconv.FormatBool(active)})

		}
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// CheckInitProfile method check the system init information
// like BIOS version, memory balanced...
func (s *ProfileServer) CheckInitProfile(profileInfo *PB.ProfileInfo,
	stream PB.ProfileMgr_CheckInitProfileServer) error {
	ch := make(chan *PB.AckCheck)
	defer close(ch)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	services := registry.GetCheckerServices()

	for _, service := range services {
		log.Infof("initializing checker service: %s", service.Name)
		if err := service.Instance.Init(); err != nil {
			return fmt.Errorf("service init failed: %v", err)
		}
	}

	// running checker service
	for _, srv := range services {
		service := srv
		checkerService, ok := service.Instance.(registry.CheckService)
		if !ok {
			continue
		}

		if registry.IsCheckDisabled(service.Instance) {
			continue
		}
		err := checkerService.Check(ch)
		if err != nil {
			log.Errorf("service %s running failed, reason: %v", service.Name, err)
			continue
		}
	}

	return nil
}

// Analysis method analysis the system traffic load
func (s *ProfileServer) Analysis(message *PB.AnalysisMessage, stream PB.ProfileMgr_AnalysisServer) error {
	if !s.TryLock() {
		return fmt.Errorf("dynamic optimizer search or analysis has been in running")
	}
	defer s.Unlock()

	_ = stream.Send(&PB.AckCheck{Name: "1. Analysis system runtime information: CPU Memory IO and Network..."})

	npipe, err := utils.CreateNamedPipe()
	if err != nil {
		return fmt.Errorf("create named pipe failed")
	}

	defer os.Remove(npipe)

	go func() {
		file, _ := os.OpenFile(npipe, os.O_RDONLY, os.ModeNamedPipe)
		reader := bufio.NewReader(file)

		scanner := bufio.NewScanner(reader)

		for scanner.Scan() {
			line := scanner.Text()
			_ = stream.Send(&PB.AckCheck{Name: line, Status: utils.INFO})
		}
	}()

	respCollectPost, err := s.collection(npipe)
	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		log.Errorf("collection system data error: %v", err)
		return err
	}

	workloadType, resourceLimit, err := s.classify(respCollectPost.Path, message.GetModel())
	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		return err
	}

	//3. judge the workload type is exist in the database
	classProfile := &sqlstore.GetClass{Class: workloadType}
	if err = sqlstore.GetClasses(classProfile); err != nil {
		log.Errorf("inquery workload type table failed %v", err)
		return fmt.Errorf("inquery workload type table failed %v", err)
	}
	if len(classProfile.Result) == 0 {
		log.Errorf("%s is not exist in the table", workloadType)
		return fmt.Errorf("%s is not exist in the table", workloadType)
	}

	// the workload type is already actived
	if classProfile.Result[0].Active {
		log.Infof("analysis result %s is the same with current active workload type", workloadType)
		return nil
	}

	//4. inquery the support app of the workload type
	classApps := &sqlstore.GetClassApp{Class: workloadType}
	err = sqlstore.GetClassApps(classApps)
	if err != nil {
		log.Errorf("inquery support app depend on class error: %v", err)
		return err
	}
	if len(classApps.Result) == 0 {
		return fmt.Errorf("class %s is not exist in the tables", workloadType)
	}
	apps := classApps.Result[0].Apps
	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("\n 2. Current System Workload Characterization is %s", apps)})
	log.Infof("workload %s support app: %s", workloadType, apps)
	log.Infof("workload %s resource limit: %s, cluster result resource limit: %s",
		workloadType, apps, resourceLimit)

	_ = stream.Send(&PB.AckCheck{Name: "\n 3. Build the best resource model..."})

	//5. get the profile type depend on the workload type
	profileType := classProfile.Result[0].ProfileType
	profileNames := strings.Split(profileType, ",")
	if len(profileNames) == 0 {
		log.Errorf("No profile or invaild profiles were specified.")
		return fmt.Errorf("no profile or invaild profiles were specified")
	}

	//6. get the profile info depend on the profile type
	log.Infof("the resource model of the profile type is %s", profileType)
	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("\n 4. Match profile: %s", profileType)})
	pro, _ := profile.Load(profileNames)
	pro.SetWorkloadType(workloadType)

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("\n 5. bengin to set static profile")})
	log.Infof("bengin to set static profile")

	//static profile setting
	ch := make(chan *PB.AckCheck)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	_ = pro.RollbackActive(ch)

	rules := &sqlstore.GetRuleTuned{Class: workloadType}
	if err := sqlstore.GetRuleTuneds(rules); err != nil {
		return err
	}

	if len(rules.Result) < 1 {
		_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("Completed optimization, please restart application!")})
		log.Info("no rules to tuned")
		return nil
	}

	log.Info("begin to dynamic tuning depending on rules")
	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("\n 6. bengin to set dynamic profile")})
	if err := tuning.RuleTuned(workloadType); err != nil {
		return err
	}

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("Completed optimization, please restart application!")})
	return nil
}

// Tuning method calling the bayes search method to tuned parameters
func (s *ProfileServer) Tuning(stream PB.ProfileMgr_TuningServer) error {
	if !s.TryLock() {
		return fmt.Errorf("dynamic optimizer search or analysis has been in running")
	}
	defer s.Unlock()

	ch := make(chan *PB.TuningMessage)
	defer close(ch)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	var optimizer = tuning.Optimizer{}
	defer optimizer.DeleteTask()

	stopCh := make(chan int, 1)
	var cycles int32 = 0
	var message string
	var step int32 = 1

	for {
		select {
		case <-stopCh:
			if cycles > 0 {
				_ = stream.Send(&PB.TuningMessage{State: PB.TuningMessage_JobInit})
			} else {
				_ = stream.Send(&PB.TuningMessage{State: PB.TuningMessage_Ending})
			}
			cycles--
		default:
		}
		if cycles < 0 {
			break
		}
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		state := reply.GetState()
		switch state {
		case PB.TuningMessage_SyncConfig:
			optimizer.Content = reply.GetContent()
			err = optimizer.SyncTunedNode(ch)
			if err != nil {
				return err
			}
		case PB.TuningMessage_JobRestart:
			log.Infof("restart cycles is: %d", cycles)
			optimizer.Content = reply.GetContent()
			if cycles > 0 {
				message = fmt.Sprintf("%d、Starting the next cycle of parameter selection......", step)
				step += 1
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
				if err = optimizer.InitFeatureSel(ch, stopCh); err != nil {
					return err
				}
			} else {
				message = fmt.Sprintf("%d、Start to tuning the system......", step)
				step += 1
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
				if err = optimizer.InitTuned(ch, stopCh); err != nil {
					return err
				}
			}
		case PB.TuningMessage_JobInit:
			project := reply.GetName()
			if len(strings.TrimSpace(project)) == 0 {
				if err != nil {
					return err
				}
				message = fmt.Sprintf("%d.Begin to Analysis the system......", step)
				step += 1
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
				project, err = s.Getworkload()
				if err != nil {
					return err
				}
				message = fmt.Sprintf("%d.Current runing application is: %s", step, project)
				step += 1
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
			}

			message = fmt.Sprintf("%d.Loading its corresponding tuning project: %s", step, project)
			step += 1
			ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}

			if err = tuning.CheckServerPrj(project, &optimizer); err != nil {
				return err
			}

			optimizer.Engine = reply.GetEngine()
			optimizer.Content = reply.GetContent()
			optimizer.Restart = reply.GetRestart()
			optimizer.RandomStarts = reply.GetRandomStarts()
			optimizer.FeatureFilterEngine = reply.GetFeatureFilterEngine()
			optimizer.FeatureFilterIters = reply.GetFeatureFilterIters()
			optimizer.SplitCount = reply.GetSplitCount()
			cycles = reply.GetFeatureFilterCycle()

			if cycles == 0 {
				if optimizer.Restart {
					message = fmt.Sprintf("%d.Continue to tuning the system......", step)
				} else {
					message = fmt.Sprintf("%d.Start to tuning the system......", step)
				}
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
				step += 1
				if err = optimizer.InitTuned(ch, stopCh); err != nil {
					return err
				}
			} else {
				message = fmt.Sprintf("%d.Starting to select the important parameters......", step)
				ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
				step += 1
				if err = optimizer.InitFeatureSel(ch, stopCh); err != nil {
					return err
				}
			}
		case PB.TuningMessage_Restore:
			project := reply.GetName()
			log.Infof("begin to restore project: %s", project)
			if err := tuning.CheckServerPrj(project, &optimizer); err != nil {
				return err
			}
			if err := optimizer.RestoreConfigTuned(ch); err != nil {
				return err
			}
			log.Infof("restore project %s success", project)
			return nil
		case PB.TuningMessage_BenchMark:
			optimizer.Content = reply.GetContent()
			err := optimizer.DynamicTuned(ch, stopCh)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

/*
UpgradeProfile method update the db file
*/
func (s *ProfileServer) UpgradeProfile(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_UpgradeProfileServer) error {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(stream.Context())
	if err != nil {
		return err
	}
	if !isLocalAddr {
		return fmt.Errorf("the upgrade command can not be remotely operated")
	}

	log.Debug("Begin to upgrade profiles\n")
	currenDbPath := path.Join(config.DatabasePath, config.DatabaseName)
	newDbPath := profileInfo.GetName()

	exist, err := utils.PathExist(config.DefaultTempPath)
	if err != nil {
		return err
	}
	if !exist {
		if err = os.MkdirAll(config.DefaultTempPath, 0750); err != nil {
			return err
		}
	}
	timeUnix := strconv.FormatInt(time.Now().Unix(), 10) + ".db"
	tempFile := path.Join(config.DefaultTempPath, timeUnix)

	if err := utils.CopyFile(tempFile, currenDbPath); err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error(), Status: utils.FAILD})
		return nil
	}

	if err := utils.CopyFile(currenDbPath, newDbPath); err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error(), Status: utils.FAILD})
		return nil
	}

	if err := sqlstore.Reload(currenDbPath); err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error(), Status: utils.FAILD})
		return nil
	}

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("upgrade success"), Status: utils.SUCCESS})
	return nil
}

/*
InfoProfile method display the content of the specified workload type
*/
func (s *ProfileServer) InfoProfile(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_InfoProfileServer) error {
	var context string
	profileName := profileInfo.GetName()
	err := filepath.Walk(config.DefaultProfilePath, func(absPath string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			absFilename := absPath[len(config.DefaultProfilePath)+1:]
			filenameOnly := strings.TrimSuffix(strings.ReplaceAll(absFilename, "/", "-"),
				path.Ext(info.Name()))
			if filenameOnly == profileName {
				data, err := ioutil.ReadFile(absPath)
				if err != nil {
					return err
				}
				context = "\n*** " + profileName + ":\n" + string(data)
				_ = stream.Send(&PB.ProfileInfo{Name: context})
				return nil
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	if context == "" {
		log.Errorf("profile %s is not exist", profileName)
		return fmt.Errorf("profile %s is not exist", profileName)
	}

	return nil
}

/*
CheckActiveProfile method check current active profile is effective
*/
func (s *ProfileServer) CheckActiveProfile(profileInfo *PB.ProfileInfo,
	stream PB.ProfileMgr_CheckActiveProfileServer) error {
	log.Debug("Begin to check active profiles\n")

	profileLogs, err := sqlstore.GetProfileLogs()
	if err != nil {
		return err
	}

	var activeName string
	if len(profileLogs) > 0 {
		activeName = profileLogs[0].ProfileID
	}

	if activeName == "" {
		return fmt.Errorf("no active profile or more than 1 active profile")
	}
	classProfile := &sqlstore.GetClass{Class: activeName}
	err = sqlstore.GetClasses(classProfile)
	if err != nil {
		return fmt.Errorf("inquery workload type table faild %v", err)
	}
	if len(classProfile.Result) > 0 {
		activeName = classProfile.Result[0].ProfileType
	}
	log.Debugf("active name is %s", activeName)

	profile, ok := profile.LoadFromProfile(activeName)

	if !ok {
		log.WithField("profile", activeName).Errorf("Load profile %s Faild", activeName)
		return fmt.Errorf("load profile %s Faild", activeName)
	}

	ch := make(chan *PB.AckCheck)
	defer close(ch)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	if err := profile.Check(ch); err != nil {
		return err
	}

	return nil
}

// ProfileRollback method rollback the profile to init state
func (s *ProfileServer) ProfileRollback(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_ProfileRollbackServer) error {
	profileLogs, err := sqlstore.GetProfileLogs()
	if err != nil {
		return err
	}

	if len(profileLogs) < 1 {
		_ = stream.Send(&PB.AckCheck{Name: "no profile need to rollback"})
		return nil
	}

	sort.Slice(profileLogs, func(i, j int) bool {
		return profileLogs[i].ID > profileLogs[j].ID
	})

	//static profile setting
	ch := make(chan *PB.AckCheck)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	for _, pro := range profileLogs {
		log.Infof("begin to restore profile id: %d", pro.ID)
		profileInfo := profile.HistoryProfile{}
		_ = profileInfo.Load(pro.Context)
		_ = profileInfo.Resume(ch)

		// delete profile log after restored
		if err := sqlstore.DelProfileLogByID(pro.ID); err != nil {
			return err
		}
		//delete backup dir
		if err := os.RemoveAll(pro.BackupPath); err != nil {
			return err
		}

		// update active profile after restored
		if err := sqlstore.ActiveProfile(pro.ProfileID); err != nil {
			return nil
		}
	}

	if err := sqlstore.InActiveProfile(); err != nil {
		return nil
	}

	return nil
}

/*
Collection method call collection script to collect system data.
*/
func (s *ProfileServer) Collection(message *PB.CollectFlag, stream PB.ProfileMgr_CollectionServer) error {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(stream.Context())
	if err != nil {
		return err
	}
	if !isLocalAddr {
		return fmt.Errorf("the collection command can not be remotely operated")
	}

	if valid := utils.IsInputStringValid(message.GetWorkload()); !valid {
		return fmt.Errorf("input:%s is invalid", message.GetWorkload())
	}

	if valid := utils.IsInputStringValid(message.GetOutputPath()); !valid {
		return fmt.Errorf("input:%s is invalid", message.GetOutputPath())
	}

	if valid := utils.IsInputStringValid(message.GetType()); !valid {
		return fmt.Errorf("input:%s is invalid", message.GetType())
	}

	if valid := utils.IsInputStringValid(message.GetBlock()); !valid {
		return fmt.Errorf("input:%s is invalid", message.GetBlock())
	}

	if valid := utils.IsInputStringValid(message.GetNetwork()); !valid {
		return fmt.Errorf("input:%s is invalid", message.GetNetwork())
	}

	classProfile := &sqlstore.GetClass{Class: message.GetType()}
	if err = sqlstore.GetClasses(classProfile); err != nil {
		return err
	}
	if len(classProfile.Result) == 0 {
		return fmt.Errorf("app type %s is not exist, use define command first", message.GetType())
	}

	profileType := classProfile.Result[0].ProfileType
	include, err := profile.GetProfileInclude(profileType)
	if err != nil {
		return err
	}

	exist, err := utils.PathExist(message.GetOutputPath())
	if err != nil {
		return err
	}
	if !exist {
		return fmt.Errorf("output_path %s is not exist", message.GetOutputPath())
	}

	if err = utils.InterfaceByName(message.GetNetwork()); err != nil {
		return err
	}

	if err = utils.DiskByName(message.GetBlock()); err != nil {
		return err
	}

	collections, err := sqlstore.GetCollections()
	if err != nil {
		log.Errorf("inquery collection tables error: %v", err)
		return err
	}

	monitors := make([]Monitor, 0)
	for _, collection := range collections {
		re := regexp.MustCompile(`\{([^}]+)\}`)
		matches := re.FindAllStringSubmatch(collection.Metrics, -1)
		if len(matches) > 0 {
			for _, match := range matches {
				if len(match) < 2 {
					continue
				}

				var value string
				if match[1] == "disk" {
					value = message.GetBlock()
				} else if match[1] == "network" {
					value = message.GetNetwork()
				} else if match[1] == "interval" {
					value = strconv.FormatInt(message.GetInterval(), 10)
				} else {
					log.Warnf("%s is not recognized", match[1])
					continue
				}
				re = regexp.MustCompile(`\{(` + match[1] + `)\}`)
				collection.Metrics = re.ReplaceAllString(collection.Metrics, value)
			}
		}

		monitor := Monitor{Module: collection.Module, Purpose: collection.Purpose, Field: collection.Metrics}
		monitors = append(monitors, monitor)
	}

	collectorBody := new(CollectorPost)
	collectorBody.SampleNum = int(message.GetDuration() / message.GetInterval())
	collectorBody.Monitors = monitors
	nowTime := time.Now().Format("20060702-150405")
	fileName := fmt.Sprintf("%s-%s.csv", message.GetWorkload(), nowTime)
	collectorBody.File = path.Join(message.GetOutputPath(), fileName)
	if include == "" {
		include = "default"
	}
	collectorBody.DataType = fmt.Sprintf("%s:%s", include, message.GetType())

	_ = stream.Send(&PB.AckCheck{Name: "start to collect data"})

	_, err = collectorBody.Post()
	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		return err
	}

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("generate %s successfully", collectorBody.File)})
	return nil
}

/*
Training method train the collected data to generate the model
*/
func (s *ProfileServer) Training(message *PB.TrainMessage, stream PB.ProfileMgr_TrainingServer) error {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(stream.Context())
	if err != nil {
		return err
	}
	if !isLocalAddr {
		return fmt.Errorf("the train command can not be remotely operated")
	}

	DataPath := message.GetDataPath()
	OutputPath := message.GetOutputPath()

	compressPath, err := utils.CreateCompressFile(DataPath)
	if err != nil {
		log.Debugf("Failed to compress %s: %v", DataPath, err)
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		return err
	}
	defer os.Remove(compressPath)

	trainPath, err := Post("training", "file", compressPath)
	if err != nil {
		log.Debugf("Failed to transfer file: %v", err)
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		return err
	}

	trainBody := new(models.Training)
	trainBody.DataPath = trainPath
	trainBody.OutputPath = OutputPath
	trainBody.ModelPath = path.Join(config.DefaultAnalysisPath, "models")

	success, err := trainBody.Post()
	if err != nil {
		return err
	}
	if success {
		_ = stream.Send(&PB.AckCheck{Name: "training the self collect data success"})
		return nil
	}

	_ = stream.Send(&PB.AckCheck{Name: "training the self collect data failed"})
	return nil
}

// Charaterization method will be deprecate in the future
func (s *ProfileServer) Charaterization(profileInfo *PB.ProfileInfo, stream PB.ProfileMgr_CharaterizationServer) error {
	_ = stream.Send(&PB.AckCheck{Name: "1. Analysis system runtime information: CPU Memory IO and Network..."})

	npipe, err := utils.CreateNamedPipe()
	if err != nil {
		return fmt.Errorf("create named pipe failed")
	}

	defer os.Remove(npipe)

	go func() {
		file, _ := os.OpenFile(npipe, os.O_RDONLY, os.ModeNamedPipe)
		reader := bufio.NewReader(file)
		scanner := bufio.NewScanner(reader)

		for scanner.Scan() {
			line := scanner.Text()
			_ = stream.Send(&PB.AckCheck{Name: line, Status: utils.INFO})
		}
	}()

	respCollectPost, err := s.collection(npipe)
	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		log.Errorf("collection system data error: %v", err)
		return err
	}

	var customeModel string
	workloadType, _, err := s.classify(respCollectPost.Path, customeModel)
	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error()})
		return err
	}
	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("\n 2. Current System Workload Characterization is %s", workloadType)})
	return nil
}

// Define method user define workload type and profile
func (s *ProfileServer) Define(ctx context.Context, message *PB.DefineMessage) (*PB.Ack, error) {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(ctx)
	if err != nil {
		return &PB.Ack{}, err
	}
	if !isLocalAddr {
		return &PB.Ack{}, fmt.Errorf("the define command can not be remotely operated")
	}

	serviceType := message.GetServiceType()
	applicationName := message.GetApplicationName()
	scenarioName := message.GetScenarioName()
	content := string(message.GetContent())
	profileName := serviceType + "-" + applicationName + "-" + scenarioName

	workloadTypeExist, err := sqlstore.ExistWorkloadType(profileName)
	if err != nil {
		return &PB.Ack{}, err
	}
	if !workloadTypeExist {
		if err = sqlstore.InsertClassApps(&sqlstore.ClassApps{
			Class:     profileName,
			Apps:      profileName,
			Deletable: true}); err != nil {
			return &PB.Ack{}, err
		}
	}

	profileNameExist, err := sqlstore.ExistProfileName(profileName)
	if err != nil {
		return &PB.Ack{}, err
	}
	if !profileNameExist {
		if err = sqlstore.InsertClassProfile(&sqlstore.ClassProfile{
			Class:       profileName,
			ProfileType: profileName,
			Active:      false}); err != nil {
			return &PB.Ack{}, err
		}
	}

	profileExist, err := profile.ExistProfile(profileName)
	if err != nil {
		return &PB.Ack{}, err
	}

	if profileExist {
		return &PB.Ack{Status: fmt.Sprintf("%s is already exist", profileName)}, nil
	}

	dstPath := path.Join(config.DefaultProfilePath, serviceType, applicationName)
	err = utils.CreateDir(dstPath, utils.FilePerm)
	if err != nil {
		return &PB.Ack{}, err
	}

	dstFile := path.Join(dstPath, fmt.Sprintf("%s.conf", scenarioName))
	err = utils.WriteFile(dstFile, content, utils.FilePerm, os.O_WRONLY|os.O_CREATE)
	if err != nil {
		log.Error(err)
		return &PB.Ack{}, err
	}

	return &PB.Ack{Status: "OK"}, nil
}

// Delete method delete the self define workload type from database
func (s *ProfileServer) Delete(ctx context.Context, message *PB.ProfileInfo) (*PB.Ack, error) {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(ctx)
	if err != nil {
		return &PB.Ack{}, err
	}
	if !isLocalAddr {
		return &PB.Ack{}, fmt.Errorf("the undefine command can not be remotely operated")
	}

	profileName := message.GetName()

	classApps := &sqlstore.GetClassApp{Class: profileName}
	if err = sqlstore.GetClassApps(classApps); err != nil {
		return &PB.Ack{}, err
	}

	if len(classApps.Result) != 1 || !classApps.Result[0].Deletable {
		return &PB.Ack{Status: "only self defined type can be deleted"}, nil
	}

	if err = sqlstore.DeleteClassApps(profileName); err != nil {
		return &PB.Ack{}, err
	}

	profileNameExist, err := sqlstore.ExistProfileName(profileName)
	if err != nil {
		return &PB.Ack{}, err
	}
	if profileNameExist {
		if err := sqlstore.DeleteClassProfile(profileName); err != nil {
			log.Errorf("delete item from class_profile table failed %v ", err)
		}
	}

	if err := profile.DeleteProfile(profileName); err != nil {
		log.Errorf("delete item from profile table failed %v", err)
	}

	return &PB.Ack{Status: "OK"}, nil
}

// Update method update the content of the specified workload type from database
func (s *ProfileServer) Update(ctx context.Context, message *PB.ProfileInfo) (*PB.Ack, error) {
	isLocalAddr, err := SVC.CheckRpcIsLocalAddr(ctx)
	if err != nil {
		return &PB.Ack{}, err
	}
	if !isLocalAddr {
		return &PB.Ack{}, fmt.Errorf("the update command can not be remotely operated")
	}

	profileName := message.GetName()
	content := string(message.GetContent())

	profileExist, err := profile.ExistProfile(profileName)
	if err != nil {
		return &PB.Ack{}, err
	}

	if !profileExist {
		return &PB.Ack{}, fmt.Errorf("profile name %s is exist", profileName)
	}

	err = profile.UpdateProfile(profileName, content)
	if err != nil {
		return &PB.Ack{}, err
	}
	return &PB.Ack{Status: "OK"}, nil
}

// Schedule cpu/irq/numa ...
func (s *ProfileServer) Schedule(message *PB.ScheduleMessage,
	stream PB.ProfileMgr_ScheduleServer) error {
	pids := message.GetApp()
	Strategy := message.GetStrategy()

	scheduler := schedule.GetScheduler()

	ch := make(chan *PB.AckCheck)
	defer close(ch)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	err := scheduler.Schedule(pids, Strategy, true, ch)

	if err != nil {
		_ = stream.Send(&PB.AckCheck{Name: err.Error(), Status: utils.FAILD})
		return err
	}

	_ = stream.Send(&PB.AckCheck{Name: "schedule finished"})
	return nil
}

func (s *ProfileServer) collection(npipe string) (*RespCollectorPost, error) {
	//1. get the dimension structure of the system data to be collected
	collections, err := sqlstore.GetCollections()
	if err != nil {
		log.Errorf("inquery collection tables error: %v", err)
		return nil, err
	}

	// 1.1 send the collect data command to the monitor service
	monitors := make([]Monitor, 0)
	for _, collection := range collections {
		re := regexp.MustCompile(`\{([^}]+)\}`)
		matches := re.FindAllStringSubmatch(collection.Metrics, -1)
		if len(matches) > 0 {
			for _, match := range matches {
				if len(match) < 2 {
					continue
				}
				var value string
				if s.Raw.Section("system").Haskey(match[1]) {
					value = s.Raw.Section("system").Key(match[1]).Value()
				} else if s.Raw.Section("server").Haskey(match[1]) {
					value = s.Raw.Section("server").Key(match[1]).Value()
				} else {
					return nil, fmt.Errorf("%s is not exist in the system or server section", match[1])
				}
				re = regexp.MustCompile(`\{(` + match[1] + `)\}`)
				collection.Metrics = re.ReplaceAllString(collection.Metrics, value)
			}
		}

		monitor := Monitor{Module: collection.Module, Purpose: collection.Purpose, Field: collection.Metrics}
		monitors = append(monitors, monitor)
	}

	sampleNum := s.Raw.Section("server").Key("sample_num").MustInt(20)
	collectorBody := new(CollectorPost)
	collectorBody.SampleNum = sampleNum
	collectorBody.Monitors = monitors
	collectorBody.File = "/run/atuned/test.csv"
	if npipe != "" {
		collectorBody.Pipe = npipe
	}

	log.Infof("tuning collector body is:", collectorBody)
	respCollectPost, err := collectorBody.Post()
	if err != nil {
		return nil, err
	}
	return respCollectPost, nil
}

func (s *ProfileServer) classify(dataPath string, customeModel string) (string, string, error) {
	//2. send the collected data to the model for completion type identification
	var resourceLimit string
	var workloadType string
	dataPath, err := Post("classification", "file", dataPath)
	if err != nil {
		log.Errorf("Failed transfer file to server: %v", err)
		return workloadType, resourceLimit, err
	}
	defer os.Remove(dataPath)

	body := new(ClassifyPostBody)
	body.Data = dataPath
	body.ModelPath = path.Join(config.DefaultAnalysisPath, "models")

	if customeModel != "" {
		body.Model = customeModel
	}
	respPostIns, err := body.Post()
	if err != nil {
		return workloadType, resourceLimit, err
	}

	log.Infof("workload: %s, cluster result resource limit: %s",
		respPostIns.WorkloadType, respPostIns.ResourceLimit)
	resourceLimit = respPostIns.ResourceLimit
	workloadType = respPostIns.WorkloadType
	return workloadType, resourceLimit, nil
}

func (s *ProfileServer) Getworkload() (string, error) {
	var npipe string
	var customeModel string
	respCollectPost, err := s.collection(npipe)
	if err != nil {
		return "", err
	}

	workload, _, err := s.classify(respCollectPost.Path, customeModel)
	if err != nil {
		return "", err
	}
	if len(workload) == 0 {
		return "", fmt.Errorf("workload is empty")
	}
	return workload, nil
}

// Generate method generate the yaml file for tuning
func (s *ProfileServer) Generate(message *PB.ProfileInfo, stream PB.ProfileMgr_GenerateServer) error {
	ch := make(chan *PB.AckCheck)
	defer close(ch)
	go func() {
		for value := range ch {
			_ = stream.Send(value)
		}
	}()

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("1.Start to analysis the system bottleneck")})
	var npipe string
	respCollectPost, err := s.collection(npipe)
	if err != nil {
		return err
	}
	log.Infof("collect data response body is: %+v", respCollectPost)
	collectData := respCollectPost.Data
	projectName := message.GetName()

	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("2.Finding potential tuning parameters")})

	var tuningData tuning.TuningData
	err = mapstructure.Decode(collectData, &tuningData)
	if err != nil {
		return err
	}
	log.Infof("decode to structure is: %+v", tuningData)

	ruleFile := path.Join(config.DefaultRulePath, config.TuningRuleFile)
	engine := tuning.NewRuleEngine(ruleFile)
	if engine == nil {
		fmt.Errorf("create rules engine failed")
	}

	tuningFile := tuning.NewTuningFile(projectName, ch)
	err = tuningFile.Load()
	if err != nil {
		return err
	}
	engine.AddContext("TuningData", &tuningData)
	engine.AddContext("TuningFile", tuningFile)
	err = engine.Execute()
	if err != nil {
		return err
	}

	if len(tuningFile.PrjSrv.Object) <= 0 {
		_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("   No tuning parameters founed")})
		return nil
	}
	dstFile := path.Join(config.DefaultTuningPath, fmt.Sprintf("%s.yaml", projectName))
	log.Infof("generate tuning file: %s", dstFile)

	err = tuningFile.Save(dstFile)
	if err != nil {
		return err
	}
	_ = stream.Send(&PB.AckCheck{Name: fmt.Sprintf("3. Generate tuning project: %s\n    project name: %s", dstFile, projectName)})

	return nil
}
