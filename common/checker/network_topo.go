/*
 * Copyright (c) 2019 Huawei Technologies Co., Ltd.
 * A-Tune is licensed under the Mulan PSL v1.
 * You can use this software according to the terms and conditions of the Mulan PSL v1.
 * You may obtain a copy of Mulan PSL v1 at:
 *     http://license.coscl.org.cn/MulanPSL
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR
 * PURPOSE.
 * See the Mulan PSL v1 for more details.
 * Create: 2019-10-29
 */

package checker

import (
	PB "atune/api/profile"
	"atune/common/config"
	"atune/common/models"
	"atune/common/registry"
	"atune/common/system"
	"atune/common/utils"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

func init() {
	registry.RegisterCheckerService("net_topo", &NetworkTopo{
		Path: path.Join(config.DefaultCheckerPath, "net_topo.xml"),
	})
}

// NetworkTopo represent the network topology type
type NetworkTopo struct {
	Path string
}

// Init NetworkTopo
func (n *NetworkTopo) Init() error {
	exist, err := utils.PathExist(n.Path)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}
	if _, err := models.MonitorGet("net", "topo", "xml", n.Path, ""); err != nil {
		return err
	}

	return nil
}

/*
Check method check the cpu information
*/
func (n *NetworkTopo) Check(ch chan *PB.AckCheck) error {
	file, err := os.Open(n.Path)
	if err != nil {
		return err
	}

	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}

	networkTopo := system.NetworkTopo{}
	err = xml.Unmarshal(data, &networkTopo)
	if err != nil {
		return err
	}

	sendChanToAdm(ch, "network information:", utils.SUCCESS, "")

	for _, network := range networkTopo.Networks {
		if network.Name == "" {
			network.Name = network.NetChild.Name
		}
		message := fmt.Sprintf("    name: %-15s   product: %s ", network.Name, network.Product)
		sendChanToAdm(ch, message, utils.SUCCESS, "")
	}

	return nil
}