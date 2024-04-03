// Copyright 2018 The CubeFS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package flashnode

import (
	"time"

	"github.com/cubefs/cubefs/proto"
)

const (
	moduleName = "flashnode"

	NodeLatestVersion            = proto.BaseVersion
	LruCacheDefaultCapacity      = 400000
	CacheReqWriteTimeoutMilliSec = 500
	CacheReqReadTimeoutMilliSec  = 500
	CacheReqConnectionTimeout    = 500 * time.Millisecond
	UpdateRateLimitInfoInterval  = 60 * time.Second
	ServerTimeOut                = 60 * 5
	ConnectPoolIdleConnTimeout   = 60 * time.Second
	DefaultBurst                 = 512
	registerMaxRetryCount        = 3
	registerRetryWaitInterval    = time.Second * 5
)

// Configuration keys
const (
	cfgLocalIP    = "localIP"
	cfgListen     = "listen"
	cfgMasterAddr = "masterAddr" // will be deprecated
	cfgTotalMem   = "totalMem"
	cfgZoneName   = "zoneName"
	cfgProfPort   = "prof"
)

const (
	TmpfsPath = "/cfs/tmpfs"
)

const (
	VolumePara = "volume"
	InodePara  = "inode"
)
