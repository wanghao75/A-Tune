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
Fields used by restful api.
"""

from flask_restful import fields

PROFILE_FIELD = {
    'profile': fields.String,
}

PROFILE_GET_FIELD = fields.Nested(PROFILE_FIELD)

PARAM_FIELD = {
    'status': fields.String,
    'value': fields.String,
}

CONFIGURATOR_PUT_FIELD = fields.Nested(PARAM_FIELD)

OPTIMIZER_FIELD = {
    'status': fields.String
}

CLASSIFICATION_FIELD = {
    'resource_limit': fields.String,
    'workload_type': fields.String,
    'percentage': fields.Float,
}

CLASSIFICATION_POST_FIELD = fields.Nested(CLASSIFICATION_FIELD)