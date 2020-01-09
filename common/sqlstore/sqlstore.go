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

package sqlstore

import (
	"atune/common/config"
	"atune/common/log"
	"fmt"
	"os"
	"path"

	"github.com/go-xorm/xorm"
	_ "github.com/mattn/go-sqlite3" //import go-sqlite3 engine
)

var globalEngine *xorm.Engine

// Sqlstore : struct for store db engine
type Sqlstore struct {
	Cfg    *config.Cfg
	engine *xorm.Engine
}

// Init method init the global db engine
func (s *Sqlstore) Init() error {
	var engine *xorm.Engine

	err := os.MkdirAll(config.DatabasePath, 0750)
	if err != nil {
		return fmt.Errorf("faild to mkdir: %s(%v)", config.DatabasePath, err)
	}

	dbName := path.Join(config.DatabasePath, config.DatabaseName)
	connStr := "file:" + dbName + "?cache=shared&mode=rwc"

	log.Infof("Connecting to DB: %s", dbName)

	engine, err = xorm.NewEngine(config.DatabaseType, connStr)
	if err != nil {
		return fmt.Errorf("faild to connect to database: %v", err)
	}

	s.engine = engine
	globalEngine = engine

	return nil
}

// Reload method, reload the db file for hot update
func Reload(path string) error {
	if globalEngine != nil {
		_ = globalEngine.Close()
	}

	connStr := "file:" + path + "?cache=shared&mode=rwc"

	log.Infof("Reload DB: %s", path)
	engine, err := xorm.NewEngine("sqlite3", connStr)
	if err != nil {
		return fmt.Errorf("faild to connect to database: %v", err)
	}

	globalEngine = engine
	return nil
}