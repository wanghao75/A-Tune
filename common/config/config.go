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

package config

import (
	"fmt"
	"gitee.com/openeuler/A-Tune/common/log"
	"gitee.com/openeuler/A-Tune/common/utils"
	"net"
	"path"
	"strings"

	"github.com/go-ini/ini"
)

var Version = "no version specified"

// application common config
const (
	EnvAddr    = "ATUNED_ADDR"
	EnvPort    = "ATUNED_PORT"
	EnvTLS     = "ATUNE_TLS"
	EnvCliCert = "ATUNE_CLICERT"

	DefaultProtocol = "unix"
	DefaultTgtAddr  = "/var/run/atuned/atuned.sock"
	DefaultTgtPort  = ""
)

// default path config
const (
	DefaultPath             = "/usr/lib/atuned/"
	DefaultModDaemonSvrPath = DefaultPath + "modules"
	DefaultProfilePath      = DefaultPath + "profiles"
	DefaultConfPath         = "/etc/atuned/"
	DefaultTuningPath       = DefaultConfPath + "tuning/"
	DefaultRulePath         = DefaultConfPath + "rules/"
	DefaultScriptPath       = "/usr/libexec/atuned/scripts"
	DefaultAnalysisPath     = "/usr/libexec/atuned/analysis"
	DefaultTempPath         = "/run/atuned"
	DefaultCheckerPath      = "/usr/share/atuned/checker/"
	DefaultBackupPath       = "/usr/share/atuned/backup/"
	DefaultTuningLogPath    = "/var/atuned"
)

// log config
const (
	Formatter = "text"
	Modes     = "syslog"
)

// python service url
const (
	Protocol   string = "http"
	APIVersion string = "v1"

	ConfiguratorURI   string = "setting"
	MonitorURI        string = "monitor"
	OptimizerURI      string = "optimizer"
	CollectorURI      string = "collector"
	ClassificationURI string = "classification"
	ProfileURI        string = "profile"
	TrainingURI       string = "training"
	TransferURI       string = "transfer"
)

// database config
const (
	DatabasePath string = "/var/lib/atuned"
	DatabaseType string = "sqlite3"
	DatabaseName string = "atuned.db"
)

// monitor config
const (
	FileFormat string = "xml"
)

//tuning config
const (
	TuningFile          string  = "tuning.log"
	TuningRuleFile      string  = "tuning_rules.grl"
	TuningRestoreConfig string  = "-tuning-restore.conf"
	DefaultTimeFormat   string  = "2006-01-02 15:04:05"
	Percent             float64 = 0.6
)

// the grpc server config
var (
	TransProtocol     string
	Address           string
	Connect           string
	Port              string
	LocalHost         string
	RestPort          string
	EngineHost        string
	EnginePort        string
	TLS               bool
	TLSServerCertFile string
	TLSServerKeyFile  string
	TLSHTTPCertFile   string
	TLSHTTPKeyFile    string
	TLSHTTPCACertFile string
)

// the system config in atuned.cnf
var (
	Network string
)

// Cfg type, the type that load the conf file
type Cfg struct {
	Raw *ini.File
}

//Load method load the default conf file
func (c *Cfg) Load() error {
	defaultConfigFile := path.Join(DefaultConfPath, "atuned.cnf")

	exist, err := utils.PathExist(defaultConfigFile)
	if err != nil {
		return err
	}
	if !exist {
		return fmt.Errorf("could not find default config file")
	}

	cfg, err := ini.Load(defaultConfigFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s, %v", defaultConfigFile, err)
	}

	c.Raw = cfg

	section := cfg.Section("server")
	TransProtocol = section.Key("protocol").MustString(DefaultProtocol)
	Address = section.Key("address").MustString(DefaultTgtAddr)
	if section.HasKey("connect") {
		Connect = section.Key("connect").MustString("")
	}

	if section.HasKey("port") {
		Port = section.Key("port").MustString(DefaultTgtPort)
	}
	LocalHost = section.Key("rest_host").MustString("localhost")
	RestPort = section.Key("rest_port").MustString("8383")
	EngineHost = section.Key("engine_host").MustString("localhost")
	EnginePort = section.Key("engine_port").MustString("3838")
	utils.RestHost = LocalHost
	utils.RestPort = RestPort

	if section.HasKey("tls") {
		TLS = section.Key("tls").MustBool(false)
	}

	if TLS {
		TLSServerCertFile = section.Key("tlsservercertfile").MustString("")
		TLSServerKeyFile = section.Key("tlsserverkeyfile").MustString("")
		TLSHTTPCertFile = section.Key("tlshttpcertfile").MustString("")
		TLSHTTPKeyFile = section.Key("tlshttpkeyfile").MustString("")
		TLSHTTPCACertFile = section.Key("tlshttpcacertfile").MustString("")
	}

	section = cfg.Section("system")
	Network = section.Key("network").MustString("")

	net, err := getValidNetwork(Network)
	if err != nil {
		return err
	}

	if Network != net {
		section.Key("network").SetValue(net)
		if err := cfg.SaveTo(defaultConfigFile); err != nil {
			return err
		}
		Network = net
	}

	if err := initLogging(cfg); err != nil {
		return err
	}

	return nil
}

//NewCfg method create the cfg struct that store the conf file
func NewCfg() *Cfg {
	return &Cfg{
		Raw: ini.Empty(),
	}
}

func initLogging(cfg *ini.File) error {
	modes := strings.Split(Modes, ",")
	err := log.InitLogger(modes, cfg)

	return err
}

// GetURL return the url
func GetURL(uri string) string {
	protocol := Protocol
	if TLS {
		protocol = "https"
	}
	if IsEnginePort(uri) {
		return fmt.Sprintf("%s://%s:%s/%s/%s", protocol, EngineHost, EnginePort, APIVersion, uri)
	}

	url := fmt.Sprintf("%s://%s:%s/%s/%s", protocol, LocalHost, RestPort, APIVersion, uri)
	return url
}

// IsEnginePort return true if using opt port and host
func IsEnginePort(uri string) bool {
	if strings.EqualFold(uri, OptimizerURI) {
		return true
	}
	if strings.EqualFold(uri, ClassificationURI) {
		return true
	}
	if strings.EqualFold(uri, TransferURI) {
		return true
	}
	if strings.EqualFold(uri, TrainingURI) {
		return true
	}

	return false
}

func getValidNetwork(network string) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	validNetwork := make([]string, 0)
	for i := 0; i < len(interfaces); i++ {
		if interfaces[i].Flags&net.FlagUp != 0 {
			address, err := interfaces[i].Addrs()
			if err != nil {
				return "", err
			}
			for _, addr := range address {
				if ip, ok := addr.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
					if network == interfaces[i].Name {
						log.Infof("valid network : %s", network)
						return network, nil
					}
					validNetwork = append(validNetwork, interfaces[i].Name)
				}
			}
		}
	}
	if len(validNetwork) == 1 {
		log.Infof("valid network : %s", validNetwork[0])
		return validNetwork[0], nil
	}

	return "", fmt.Errorf("please provide the valid network config in atuned.cnf")
}
