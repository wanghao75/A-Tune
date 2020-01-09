#!/usr/bin/python3
# -*- coding: utf-8 -*-
# Copyright (c) 2019 Huawei Technologies Co., Ltd.
# A-Tune is licensed under the Mulan PSL v1.
# You can use this software according to the terms and conditions of the Mulan PSL v1.
# You may obtain a copy of Mulan PSL v1 at:
#     http://license.coscl.org.cn/MulanPSL
# THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR
# PURPOSE.
# See the Mulan PSL v1 for more details.
# Create: 2019-10-29

"""
The sub class of the monitor, used to collect the nic estat info.
"""
import inspect
import logging
import subprocess
import getopt
import re
from ..common import Monitor

LOGGER = logging.getLogger(__name__)


class NetEStat(Monitor):
    """To collect the nic estat info"""
    _module = "NET"
    _purpose = "ESTAT"
    _option = "-n EDEV {int} 1"

    def __init__(self, user=None):
        Monitor.__init__(self, user)
        self.__cmd = "sar"
        self.__interval = 1
        self.decode.__func__.__doc__ = Monitor.decode.__doc__ % (
            "--nic=x, --fields=time/nic/rxerrs/txerrs/colls/rxdrops/"
            "txdrops/txcarrs/rxframs/rxfifos/txfifos/errs/util")

    def _get(self, para=None):
        if para is not None:
            opts, _ = getopt.getopt(para.split(), None, ['interval='])
            for opt, val in opts:
                if opt in '--interval':
                    if val.isdigit():
                        self.__interval = int(val)
                    else:
                        err = ValueError(
                            "Invalid parameter: {opt}={val}".format(
                                opt=opt, val=val))
                        LOGGER.error("%s.%s: %s", self.__class__.__name__,
                                     inspect.stack()[0][3], str(err))
                        raise err
                    continue

        output = subprocess.check_output(
            "{cmd} {opt}".format(
                cmd=self.__cmd,
                opt=self._option.format(
                    int=self.__interval)).split())
        return output.decode()

    def decode(self, info, para):
        """
        decode the result of the operation
        :param info:  content that needs to be decoded
        :param para:  command line argument
        :returns ret:  operation result
        """
        if para is None:
            return info

        keyword = {"time": 0,
                   "nic": 1,
                   "rxerrs": 2,
                   "txerrs": 3,
                   "colls": 4,
                   "rxdrops": 5,
                   "txdrops": 6,
                   "txcarrs": 7,
                   "rxframs": 8,
                   "rxfifos": 9,
                   "txfifos": 10,
                   "errs": "errs",
                   "util": "util"}

        keys = []
        nic = "e.*?"
        ret = ""

        opts, _ = getopt.getopt(para.split(), None, ['nic=', 'fields='])
        for opt, val in opts:
            if opt in '--nic':
                nic = val
                continue
            if opt in '--fields':
                keys.append(keyword[val])
                continue

        pattern = re.compile(
            r"^(\d.*?)\ {1,}(" +
            nic +
            r")\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)"
            r"\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)\ {1,}(\d*\.?\d*)"
            r"\ {2,}(\d*\.?\d*)",
            re.ASCII | re.MULTILINE)
        search_obj = pattern.findall(info)
        if len(search_obj) == 0:
            err = LookupError("Fail to find data for {}".format(nic))
            LOGGER.error("%s.%s: %s", self.__class__.__name__,
                         inspect.stack()[0][3], str(err))
            raise err

        for i in keys:
            if type(i).__name__ == 'int':
                ret = ret + " " + search_obj[-1][i]
            elif i == "errs":
                errs = float(search_obj[-1][keyword["rxerrs"]]) + \
                       float(search_obj[-1][keyword["txerrs"]])
                ret = ret + " " + str(errs)
            elif i == "util":
                util = float(search_obj[-1][keyword["rxdrops"]]) + \
                       float(search_obj[-1][keyword["txdrops"]]) + \
                       float(search_obj[-1][keyword["rxfifos"]]) + \
                       float(search_obj[-1][keyword["txfifos"]])
                ret = ret + " " + str(util)
        return ret