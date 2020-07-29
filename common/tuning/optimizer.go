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

package tuning

import (
	"bufio"
	"fmt"
	PB "gitee.com/openeuler/A-Tune/api/profile"
	"gitee.com/openeuler/A-Tune/common/client"
	"gitee.com/openeuler/A-Tune/common/config"
	"gitee.com/openeuler/A-Tune/common/http"
	"gitee.com/openeuler/A-Tune/common/log"
	"gitee.com/openeuler/A-Tune/common/models"
	"gitee.com/openeuler/A-Tune/common/project"
	"gitee.com/openeuler/A-Tune/common/utils"
	"golang.org/x/net/context"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Optimizer : the type implement the bayes serch service
type Optimizer struct {
	Prj                 *project.YamlPrjSvr
	Content             []byte
	Iter                int
	MaxIter             int32
	FeatureFilterIters  int32
	RandomStarts        int32
	OptimizerPutURL     string
	FinalEval           string
	Engine              string
	FeatureFilterEngine string
	TuningFile          string
	MinEvalSum          float64
	EvalBase            string
	RespPutIns          *models.RespPutBody
	StartIterTime       string
	TotalTime           float64
	Percentage          float64
	BackupFlag          bool
	FeatureFilter       bool
	Restart             bool
}

// InitTuned method for iniit tuning
func (o *Optimizer) InitTuned(ch chan *PB.TuningMessage, stopCh chan int) error {
	o.FeatureFilter = false
	clientIter, err := strconv.Atoi(string(o.Content))
	if err != nil {
		return err
	}

	log.Infof("begin to dynamic optimizer search, client ask iterations:%d", clientIter)

	//dynamic profle setting
	o.MaxIter = int32(clientIter)
	if o.MaxIter > o.Prj.Maxiterations {
		o.MaxIter = o.Prj.Maxiterations
		log.Infof("project:%s max iterations:%d", o.Prj.Project, o.Prj.Maxiterations)
		ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(fmt.Sprintf("server project %s max iterations %d\n",
			o.Prj.Project, o.Prj.Maxiterations))}

	}

	if err := utils.CreateDir(config.DefaultTempPath, 0750); err != nil {
		return err
	}

	o.TuningFile = path.Join(config.DefaultTempPath, fmt.Sprintf("%s_%s", o.Prj.Project, config.TuningFile))
	projectName := fmt.Sprintf("project %s\n", o.Prj.Project)
	err = utils.WriteFile(o.TuningFile, projectName, utils.FilePerm,
		os.O_WRONLY|os.O_CREATE|os.O_APPEND)
	if err != nil {
		log.Error(err)
		return err
	}

	if !o.BackupFlag {
		err = o.Backup(ch)
		if err != nil {
			return err
		}
	}
	if err := o.createOptimizerTask(ch, o.MaxIter, o.Engine); err != nil {
		return err
	}

	o.Content = nil
	if err := o.DynamicTuned(ch, stopCh); err != nil {
		return err
	}
	return nil
}

func (o *Optimizer) createOptimizerTask(ch chan *PB.TuningMessage, iters int32, engine string) error {
	optimizerBody := new(models.OptimizerPostBody)
	if o.Restart {
		o.readTuningLog(optimizerBody)
	}
	if iters <= int32(len(optimizerBody.Xref)) {
		return fmt.Errorf("create task failed for client ask iters less than tuning history")
	}
	optimizerBody.MaxEval = iters - int32(len(optimizerBody.Xref))
	optimizerBody.Engine = engine
	optimizerBody.RandomStarts = o.RandomStarts
	optimizerBody.Knobs = make([]models.Knob, 0)
	for _, item := range o.Prj.Object {
		if item.Info.Skip {
			continue
		}
		knob := new(models.Knob)
		knob.Dtype = item.Info.Dtype
		knob.Name = item.Name
		knob.Type = item.Info.Type
		knob.Ref = item.Info.Ref
		knob.Range = item.Info.Scope
		knob.Items = item.Info.Items
		knob.Step = item.Info.Step
		knob.Options = item.Info.Options
		optimizerBody.Knobs = append(optimizerBody.Knobs, *knob)
	}

	log.Infof("optimizer post body is: %+v", optimizerBody)
	respPostIns, err := optimizerBody.Post()
	if err != nil {
		return err
	}
	if respPostIns.Status != "OK" {
		err := fmt.Errorf("create task failed: %s", respPostIns.Status)
		log.Errorf(err.Error())
		return err
	}

	url := config.GetURL(config.OptimizerURI)
	o.OptimizerPutURL = fmt.Sprintf("%s/%s", url, respPostIns.TaskID)
	log.Infof("optimizer put url is: %s", o.OptimizerPutURL)

	return nil
}

func (o *Optimizer) readTuningLog(body *models.OptimizerPostBody) {
	file, err := os.Open(o.TuningFile)
	if err != nil {
		log.Error(err)
		return
	}
	defer file.Close()

	var startTime time.Time
	var endTime time.Time
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		items := strings.Split(line, "|")
		if len(items) != 5 {
			continue
		}
		o.Iter, _ = strconv.Atoi(items[0])
		yFloat, _ := utils.CalculateBenchMark(items[3])
		if o.Iter == 1 {
			o.EvalBase = fmt.Sprintf("%s=%.2f", project.BASE_BENCHMARK_VALUE, yFloat)
			o.MinEvalSum = yFloat
		}
		startTime, _ = time.Parse(config.DefaultTimeFormat, items[1])
		endTime, _ = time.Parse(config.DefaultTimeFormat, items[2])
		o.TotalTime = o.TotalTime + endTime.Sub(startTime).Seconds()

		if yFloat < o.MinEvalSum {
			o.MinEvalSum = yFloat
		}
		xPara := strings.Split(items[4], ",")
		xValue := make([]string, 0)
		for _, para := range xPara {
			xValue = append(xValue, para)
		}

		body.Xref = append(body.Xref, xValue)
		body.Yref = append(body.Yref, strconv.FormatFloat(yFloat, 'f', -1, 64))
	}
}

/*
DynamicTuned method using bayes algorithm to search the best performance parameters
*/
func (o *Optimizer) DynamicTuned(ch chan *PB.TuningMessage, stopCh chan int) error {
	var evalValue string
	var err error
	if o.Content != nil {
		evalValue, err = o.evalParsing(ch)
		if err != nil {
			return err
		}
	}

	os.Setenv("ITERATION", strconv.Itoa(o.Iter))

	optPutBody := new(models.OptimizerPutBody)
	optPutBody.Iterations = o.Iter
	optPutBody.Value = evalValue
	log.Infof("optimizer put body is: %+v", optPutBody)
	o.RespPutIns, err = optPutBody.Put(o.OptimizerPutURL)
	if err != nil {
		log.Errorf("get setting parameter error: %v", err)
		return err
	}

	log.Infof("optimizer put response body: %+v", o.RespPutIns)

	if !o.matchRelations(o.RespPutIns.Param) && !o.RespPutIns.Finished {
		ch <- &PB.TuningMessage{State: PB.TuningMessage_Threshold}
		return nil
	}

	err, scripts := o.Prj.RunSet(o.RespPutIns.Param)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Info("set the parameter success")

	err = syncConfigToOthers(scripts)
	if err != nil {
		return err
	}

	err, scripts = o.Prj.RestartProject()
	if err != nil {
		log.Error(err)
		return err
	}
	log.Info("restart project success")

	err = syncConfigToOthers(scripts)
	if err != nil {
		return err
	}

	o.StartIterTime = time.Now().Format(config.DefaultTimeFormat)

	if o.RespPutIns.Finished {
		finalEval := strings.Replace(o.FinalEval, "=-", "=", -1)
		optimizationTerm := fmt.Sprintf("\n The final optimization result is: %s\n"+
			" The final evaluation value is: %s\n", o.RespPutIns.Param, finalEval)
		log.Info(optimizationTerm)
		ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(optimizationTerm)}

		remainParams := o.filterParams()
		if o.FeatureFilter {
			message := fmt.Sprintf(" The selected most important parameters is:\n"+
				" %s(%d->%d)\n", remainParams,
				len(strings.Split(o.RespPutIns.Param, ",")), len(strings.Split(remainParams, ",")))
			ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
		}

		stopCh <- 1
		o.Iter = 0
		if err = deleteTask(o.OptimizerPutURL); err != nil {
			log.Error(err)
		}
		return nil
	}

	o.Iter++
	if int32(o.Iter) <= o.MaxIter {
		message := fmt.Sprintf("Current Tuning Progress.....(%d/%d)", o.Iter, o.MaxIter)
		ch <- &PB.TuningMessage{State: PB.TuningMessage_Display, Content: []byte(message)}
	}
	evalMinSum := fmt.Sprintf("%s=%.2f", project.MIN_BENCHMARK_VALUE, o.MinEvalSum)
	log.Infof("send back to client to start benchmark")
	ch <- &PB.TuningMessage{State: PB.TuningMessage_BenchMark,
		Content:   []byte(o.RespPutIns.Param),
		TuningLog: &PB.TuningHistory{BaseEval: o.EvalBase, MinEval: evalMinSum, TotalTime: int64(o.TotalTime), Starts: int32(o.Iter)}}
	return nil
}

func (o *Optimizer) matchRelations(optStr string) bool {
	return o.Prj.MatchRelations(optStr)
}

func (o *Optimizer) filterParams() string {
	log.Infof("params importance weight is: %s", o.RespPutIns.Rank)
	sortedParams := make(utils.SortedPair, 0)
	paramList := strings.Split(o.RespPutIns.Rank, ",")

	if len(paramList) == 0 {
		return ""
	}

	for _, param := range paramList {
		paramPair := strings.Split(param, ":")
		if len(paramPair) != 2 {
			continue
		}
		name := strings.TrimSpace(paramPair[0])
		score, _ := strconv.ParseFloat(strings.TrimSpace(paramPair[1]), 64)
		sortedParams = append(sortedParams, utils.Pair{Name: name, Score: score})
	}
	sort.Sort(sortedParams)
	log.Infof("sorted params: %+v", sortedParams)

	var skipIndex int
	skipIndex = int(float64(len(sortedParams)) * o.Percentage)
	if len(sortedParams) <= 10 {
		skipIndex = len(sortedParams)
	}
	skipParams := sortedParams[skipIndex:]
	remaindParams := sortedParams[:skipIndex]

	skipMap := make(map[string]struct{})
	for _, param := range skipParams {
		skipMap[param.Name] = struct{}{}
	}
	for _, item := range o.Prj.Object {
		if _, ok := skipMap[item.Name]; ok {
			item.Info.Skip = true
		}
	}
	tuningParams := make([]string, 0)
	for _, param := range remaindParams {
		tuningParams = append(tuningParams, fmt.Sprintf("%s:%.2f", param.Name, param.Score))
	}
	return strings.Join(tuningParams, ",")
}

//restore tuning config
func (o *Optimizer) RestoreConfigTuned(ch chan *PB.TuningMessage) error {
	tuningRestoreConf := path.Join(config.DefaultTempPath, o.Prj.Project+config.TuningRestoreConfig)
	exist, err := utils.PathExist(tuningRestoreConf)
	if err != nil {
		return err
	}
	if !exist {
		err := fmt.Errorf("%s project has not been executed "+
			"the dynamic optimizer search", o.Prj.Project)
		log.Errorf(err.Error())
		return err
	}

	content, err := ioutil.ReadFile(tuningRestoreConf)
	if err != nil {
		log.Error(err)
		return err
	}

	log.Infof("restoring params is: %s", string(content))
	err, scripts := o.Prj.RunSet(string(content))
	if err != nil {
		log.Error(err)
		return err
	}

	if err := syncConfigToOthers(scripts); err != nil {
		return err
	}

	result := fmt.Sprintf("restore %s project params success", o.Prj.Project)
	ch <- &PB.TuningMessage{State: PB.TuningMessage_Ending, Content: []byte(result)}
	log.Infof(result)
	return nil
}

func (o *Optimizer) evalParsing(ch chan *PB.TuningMessage) (string, error) {
	eval := string(o.Content)

	endIterTime := time.Now().Format(config.DefaultTimeFormat)
	iterInfo := make([]string, 0)
	iterInfo = append(iterInfo, strconv.Itoa(o.Iter), o.StartIterTime, endIterTime,
		eval, o.RespPutIns.Param)
	output := strings.Join(iterInfo, "|")
	err := utils.WriteFile(o.TuningFile, output+"\n", utils.FilePerm,
		os.O_APPEND|os.O_WRONLY)
	if err != nil {
		log.Error(err)
		return "", err
	}

	evalValue := make([]string, 0)
	evalSum := 0.0
	for _, benchStr := range strings.Split(eval, ",") {
		kvs := strings.Split(benchStr, "=")
		if len(kvs) < 2 {
			continue
		}

		floatEval, err := strconv.ParseFloat(kvs[1], 64)
		if err != nil {
			log.Error(err)
			return "", err
		}

		evalSum += floatEval
		evalValue = append(evalValue, kvs[1])
	}

	if o.Iter == 1 || evalSum < o.MinEvalSum {
		o.MinEvalSum = evalSum
		o.FinalEval = eval
	}
	return strings.Join(evalValue, ","), nil
}

func deleteTask(url string) error {
	resp, err := http.Delete(url)
	if err != nil {
		log.Error("delete task failed:", err)
		return err
	}
	resp.Body.Close()
	return nil
}

//check server prj
func CheckServerPrj(data string, optimizer *Optimizer) error {
	projects := strings.Split(data, ",")

	log.Infof("client ask project: %s", data)
	var requireProject map[string]struct{}
	requireProject = make(map[string]struct{})
	for _, project := range projects {
		requireProject[strings.TrimSpace(project)] = struct{}{}
	}

	var prjs []*project.YamlPrjSvr
	err := filepath.Walk(config.DefaultTuningPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			prj := new(project.YamlPrjSvr)
			if err := utils.ParseFile(path, "yaml", &prj); err != nil {
				return fmt.Errorf("load %s failed, err: %v", path, err)
			}
			log.Infof("project:%s load %s success", prj.Project, path)
			prjs = append(prjs, prj)
		}
		return nil
	})

	if err != nil {
		return err
	}

	for _, prj := range prjs {
		if _, ok := requireProject[prj.Project]; !ok {
			continue
		}

		log.Infof("find Project:%s", prj.Project)
		if optimizer.Prj == nil {
			optimizer.Prj = prj
			continue
		}
		optimizer.Prj.MergeProject(prj)
	}

	if optimizer.Prj == nil {
		return fmt.Errorf("project:%s not found", data)
	}

	log.Debugf("optimizer objects: %+v", optimizer.Prj)
	optimizer.Percentage = config.Percent
	return nil
}

//sync tuned node
func (o *Optimizer) SyncTunedNode(ch chan *PB.TuningMessage) error {
	log.Infof("setting params is: %s", string(o.Content))
	commands := strings.Split(string(o.Content), ",")
	for _, command := range commands {
		_, err := project.ExecCommand(command)
		if err != nil {
			return fmt.Errorf("failed to exec %s, err: %v", command, err)
		}
	}

	log.Info("set the parameter success")
	ch <- &PB.TuningMessage{State: PB.TuningMessage_Ending}

	return nil
}

//sync config to other nodes in cluster mode
func syncConfigToOthers(scripts string) error {
	if config.TransProtocol != "tcp" || scripts == "" {
		return nil
	}

	otherServers := strings.Split(strings.TrimSpace(config.Connect), ",")
	log.Infof("sync other nodes: %s", otherServers)

	for _, server := range otherServers {
		if server == config.Address || server == "" {
			continue
		}
		if err := syncConfigToNode(server, scripts); err != nil {
			log.Errorf("server %s failed to sync config, err: %v", server, err)
			return err
		}
	}
	return nil
}

//sync config to server node
func syncConfigToNode(server string, scripts string) error {
	c, err := client.NewClient(server, config.Port)
	if err != nil {
		return err
	}

	defer c.Close()
	svc := PB.NewProfileMgrClient(c.Connection())
	stream, err := svc.Tuning(context.Background())
	if err != nil {
		return err
	}

	defer stream.CloseSend()
	content := &PB.TuningMessage{State: PB.TuningMessage_SyncConfig, Content: []byte(scripts)}
	if err := stream.Send(content); err != nil {
		return fmt.Errorf("sends failure, error: %v", err)
	}

	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		state := reply.GetState()
		if state == PB.TuningMessage_Ending {
			log.Infof("server %s reply status success", server)
			break
		}
	}

	return nil
}

// InitFeatureSel method for init feature selection tuning
func (o *Optimizer) InitFeatureSel(ch chan *PB.TuningMessage, stopCh chan int) error {
	o.FeatureFilter = true
	if err := utils.CreateDir(config.DefaultTempPath, 0750); err != nil {
		return err
	}

	o.TuningFile = path.Join(config.DefaultTempPath, fmt.Sprintf("%s_%s", o.Prj.Project, config.TuningFile))
	projectName := fmt.Sprintf("project %s\n", o.Prj.Project)
	err := utils.WriteFile(o.TuningFile, projectName, utils.FilePerm,
		os.O_WRONLY|os.O_CREATE|os.O_APPEND)
	if err != nil {
		log.Error(err)
		return err
	}

	err = o.Backup(ch)
	if err != nil {
		return err
	}
	if err := o.createOptimizerTask(ch, o.FeatureFilterIters, o.FeatureFilterEngine); err != nil {
		return err
	}

	o.Content = nil
	if err := o.DynamicTuned(ch, stopCh); err != nil {
		return err
	}
	return nil
}

// Backup method for backup the init config of tuning params
func (o *Optimizer) Backup(ch chan *PB.TuningMessage) error {
	o.BackupFlag = true
	initConfigure := make([]string, 0)
	for _, item := range o.Prj.Object {
		out, err := project.ExecCommand(item.Info.GetScript)
		if err != nil {
			return fmt.Errorf("faild to exec %s, err: %v", item.Info.GetScript, err)
		}
		initConfigure = append(initConfigure, strings.TrimSpace(item.Name+"="+string(out)))
	}

	err := utils.WriteFile(path.Join(config.DefaultTempPath,
		o.Prj.Project+config.TuningRestoreConfig), strings.Join(initConfigure, ","),
		utils.FilePerm, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

// DeleteTask method delete the optimizer task in runing
func (o *Optimizer) DeleteTask() error {
	if o.OptimizerPutURL == "" {
		return nil
	}

	if err := deleteTask(o.OptimizerPutURL); err != nil {
		return err
	}
	log.Infof("delete task %s success!", o.OptimizerPutURL)
	o.OptimizerPutURL = ""
	return nil
}
