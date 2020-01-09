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

package models

import (
	"atune/common/config"
	"atune/common/http"
	"encoding/json"
	"io/ioutil"
)

// OptimizerPostBody send to the service to create a optimizer task
type OptimizerPostBody struct {
	MaxEval int    `json:"max_eval"`
	Knobs   []Knob `json:"knobs"`
}

// Knob body store the tuning properties
type Knob struct {
	Dtype   string   `json:"dtype"`
	Name    string   `json:"name"`
	Options []string `json:"options"`
	Type    string   `json:"type"`
	Range   []int64  `json:"range"`
	Items   []int64  `json:"items"`
	Step    int64    `json:"step"`
	Ref     string   `json:"ref"`
}

// RespPostBody :the body returned of create optimizer task
type RespPostBody struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// OptimizerPutBody send to the optimizer service when iterations
type OptimizerPutBody struct {
	Iterations int    `json:"iterations"`
	Value      string `json:"value"`
}

// RespPutBody :the body returned of each optimizer iteration
type RespPutBody struct {
	Param string `json:"param"`
}

// Post method create a optimizer task
func (o *OptimizerPostBody) Post() (*RespPostBody, error) {
	url := config.GetURL(config.OptimizerURI)
	res, err := http.Post(url, o)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	respBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	respPostIns := new(RespPostBody)

	err = json.Unmarshal(respBody, respPostIns)
	if err != nil {
		return nil, err
	}

	return respPostIns, nil
}

// Put method send benchmark result to optimizer service
func (o *OptimizerPutBody) Put(url string) (*RespPutBody, error) {
	res, err := http.Put(url, o)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	respBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	respPutIns := new(RespPutBody)

	err = json.Unmarshal(respBody, respPutIns)
	if err != nil {
		return nil, err
	}

	return respPutIns, nil
}