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
	"context"
	"fmt"
	"github.com/cubefs/cubefs/util/iputil"
	"github.com/cubefs/cubefs/util/ping"
	"github.com/cubefs/cubefs/util/single_context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cubefs/cubefs/cmd/common"
	"github.com/cubefs/cubefs/proto"
	masterSDK "github.com/cubefs/cubefs/sdk/master"
	"github.com/cubefs/cubefs/storage/cache_engine"
	"github.com/cubefs/cubefs/util/config"
	"github.com/cubefs/cubefs/util/connpool"
	"github.com/cubefs/cubefs/util/errors"
	"github.com/cubefs/cubefs/util/exporter"
	"github.com/cubefs/cubefs/util/log"
	"github.com/cubefs/cubefs/util/memory"
	"github.com/cubefs/cubefs/util/statinfo"
	"github.com/cubefs/cubefs/util/statistics"
	"golang.org/x/time/rate"
)

var ModuleName = "flashNode"
var gSingleContext *single_context.SingleContext

// The FlashNode manages the inode block cache to speed the file reading.
type FlashNode struct {
	nodeId          uint64
	listen          string
	profPort        string
	cacheEngine     *cache_engine.CacheEngine
	localAddr       string
	clusterId       string
	tmpfsPath       string
	zoneName        string
	total           uint64
	monitorData     []*statistics.MonitorData
	stopTcpServerC  chan uint8
	processStatInfo *statinfo.ProcessStatInfo
	connPool        *connpool.ConnectPool
	readSource      *ReadSource
	stopCh          chan bool
	netListener     net.Listener
	currentCtx      context.Context
	statistics      sync.Map // volume(string) -> []*statistics.MonitorData
	control         common.Control
	volLimitMap     map[string]uint64        // volume -> limit
	volLimiterMap   map[string]*rate.Limiter // volume -> *Limiter
	nodeLimiter     *rate.Limiter
	nodeLimit       uint64
	sync.RWMutex
}

var (
	clusterInfo  *proto.ClusterInfo
	masterClient *masterSDK.MasterClient
)

// Start starts up the flash node with the specified configuration.
//  1. Start and load each flash partition from the snapshot.
//  2. Restore raftStore fsm of each flash node range.
//  3. Start server and accept connection from the master and clients.
func (f *FlashNode) Start(cfg *config.Config) (err error) {
	return f.control.Start(f, cfg, doStart)
}

// Shutdown stops the flash node.
func (f *FlashNode) Shutdown() {
	f.control.Shutdown(f, doShutdown)
}

// Sync blocks the invoker's goroutine until the flash node shuts down.
func (f *FlashNode) Sync() {
	f.control.Sync()
}

func doStart(s common.Server, cfg *config.Config) (err error) {
	f, ok := s.(*FlashNode)
	if !ok {
		return errors.New("Invalid Node Type!")
	}
	if err = f.parseConfig(cfg); err != nil {
		return
	}
	f.stopCh = make(chan bool, 0)
	f.tmpfsPath = TmpfsPath
	if err = f.register(); err != nil {
		return
	}
	f.initLimiter()
	f.connPool = connpool.NewConnectPoolWithTimeout(ConnectPoolIdleConnTimeout, CacheReqConnectionTimeout)
	if err = f.registerAPIHandler(); err != nil {
		return
	}
	go f.startUpdateScheduler()

	err = ping.StartDefaultClient(func() ([]string, error) {
		dataNodes := make([]string, 0)
		cluster, e := masterClient.AdminAPI().GetCluster()
		if e != nil {
			return nil, e
		}
		for _, n := range cluster.DataNodes {
			dataNodes = append(dataNodes, n.Addr)
		}
		return dataNodes, nil
	})
	if err != nil {
		return
	}
	exporter.Init(exporter.NewOptionFromConfig(cfg).WithCluster(f.clusterId).WithModule(moduleName).WithZone(f.zoneName))
	if err = f.startTcpServer(); err != nil {
		return
	}
	f.readSource = NewReadSource()
	gSingleContext = single_context.NewSingleContextWithTimeout(single_context.DefaultTimeoutMs)
	if err = f.startCacheEngine(); err != nil {
		return
	}
	statistics.InitStatistics(cfg, f.clusterId, statistics.ModelFlashNode, f.zoneName, f.localAddr, f.rangeMonitorData)
	f.startUpdateProcessStatInfo()
	return
}

func doShutdown(s common.Server) {
	f, ok := s.(*FlashNode)
	if !ok {
		return
	}
	close(f.stopCh)
	// shutdown node and release the resource
	f.stopServer()
	f.stopCacheEngine()
	ping.StopDefaultClient()
	gSingleContext.Stop()
}

func (f *FlashNode) parseConfig(cfg *config.Config) (err error) {
	if cfg == nil {
		err = errors.New("invalid configuration")
		return
	}
	f.localAddr = cfg.GetString(cfgLocalIP)
	f.listen = cfg.GetString(cfgListen)
	f.profPort = cfg.GetString(cfgProfPort)
	f.zoneName = cfg.GetString(cfgZoneName)
	f.total, err = strconv.ParseUint(cfg.GetString(cfgTotalMem), 10, 64)
	if err != nil {
		return err
	}
	if f.total == 0 {
		return fmt.Errorf("bad totalMem config,Recommended to be configured as 80 percent of physical machine memory")
	}
	total, _, err := memory.GetMemInfo()
	if err == nil && f.total > uint64(float64(total)*0.8) {
		return fmt.Errorf("bad totalMem config,Recommended to be configured as 80 percent of physical machine memory")
	}
	if f.listen == "" {
		return fmt.Errorf("bad listen config")
	}
	log.LogInfof("[parseConfig] load localAddr[%v].", f.localAddr)
	log.LogInfof("[parseConfig] load listen[%v].", f.listen)
	log.LogInfof("[parseConfig] load zoneName[%v].", f.zoneName)

	masterDomain, masterAddrs := f.parseMasterAddrs(cfg)
	masterClient = masterSDK.NewMasterClientWithDomain(masterDomain, masterAddrs, false)
	log.LogInfof("[parseConfig] master addr[%v].", masterAddrs)
	log.LogInfof("[parseConfig] master domain[%v].", masterDomain)
	err = f.validConfig()
	return
}

func (f *FlashNode) parseMasterAddrs(cfg *config.Config) (masterDomain string, masterAddrs []string) {
	var err error
	masterDomain = cfg.GetString(proto.MasterDomain)
	if masterDomain != "" && !strings.Contains(masterDomain, ":") {
		masterDomain = masterDomain + ":" + proto.MasterDefaultPort
	}

	masterAddrs, err = iputil.LookupHost(masterDomain)
	if err != nil {
		masterAddrs = cfg.GetStringSlice(proto.MasterAddr)
	}
	return
}

func (f *FlashNode) validConfig() (err error) {
	if len(strings.TrimSpace(f.listen)) == 0 {
		err = errors.New("illegal listen")
		return
	}
	if len(masterClient.Nodes()) == 0 {
		err = errors.New("master address list is empty")
		return
	}
	return
}

func (f *FlashNode) stopCacheEngine() {
	if f.cacheEngine != nil {
		err := f.cacheEngine.Stop()
		if err != nil {
			log.LogErrorf("stopCacheEngine: err:%v", err)
		}
	}
}

func (f *FlashNode) startCacheEngine() (err error) {
	if f.cacheEngine, err = cache_engine.NewCacheEngine(f.tmpfsPath, int64(f.total), cache_engine.DefaultCacheMaxUsedRatio, LruCacheDefaultCapacity, time.Hour, f.readSource.ReadExtentData, f.UpdateMonitorData); err != nil {
		log.LogErrorf("start CacheEngine failed: %v", err)
		return
	}
	f.cacheEngine.Start()
	return
}

func (f *FlashNode) initLimiter() {
	f.nodeLimit = 0
	f.nodeLimiter = rate.NewLimiter(rate.Inf, DefaultBurst)
	f.volLimitMap = make(map[string]uint64)
	f.volLimiterMap = make(map[string]*rate.Limiter)
}

func (f *FlashNode) register() (err error) {
	var (
		regInfo = &masterSDK.RegNodeInfoReq{
			Role:     proto.RoleFlash,
			ZoneName: f.zoneName,
			Version:  NodeLatestVersion,
			SrvPort:  f.listen,
		}
		regRsp *proto.RegNodeRsp
	)

	for retryCount := registerMaxRetryCount; retryCount > 0; retryCount-- {
		regRsp, err = masterClient.RegNodeInfo(proto.AuthFilePath, regInfo)
		if err == nil {
			break
		}
		time.Sleep(registerRetryWaitInterval)
	}
	if err != nil {
		log.LogErrorf("FlashNode register failed: %v", err)
		return
	}

	f.nodeId = regRsp.Id
	ipAddr := strings.Split(regRsp.Addr, ":")[0]
	f.localAddr = ipAddr
	f.clusterId = regRsp.Cluster
	if err = iputil.VerifyLocalIP(f.localAddr); err != nil {
		log.LogErrorf("FlashNode register verify local ip failed: %v", err)
		return
	}
	return
}

func (f *FlashNode) startUpdateProcessStatInfo() {
	f.processStatInfo = statinfo.NewProcessStatInfo()
	f.processStatInfo.ProcessStartTime = time.Now().Format("2006-01-02 15:04:05")
	go f.processStatInfo.UpdateStatInfoSchedule()
}
