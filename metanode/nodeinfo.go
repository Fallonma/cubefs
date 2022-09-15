package metanode

import (
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/util/log"
	"github.com/chubaofs/chubaofs/util/statistics"
	"golang.org/x/time/rate"
)

const (
	UpdateDeleteLimitInfoTicket          = 1 * time.Minute
	UpdateRateLimitInfoTicket            = 5 * time.Minute
	UpdateClusterViewTicket              = 24 * time.Hour
	DefaultDeleteBatchCounts             = 128
	DefaultReqLimitBurst                 = 512
	DefaultDumpWaterLevel                = 100
	DefaultRocksDBModeMaxFsUsedPercent   = 60
	DefaultMemModeMaxFsUsedFactorPercent = 80
)

type NodeInfo struct {
	deleteBatchCount 	uint64
	readDirLimitNum		uint64
	dumpWaterLevel      uint64
}

var (
	nodeInfo                   = &NodeInfo{}
	nodeInfoStopC              = make(chan struct{}, 0)
	deleteWorkerSleepMs uint64 = 0

	// request rate limiter for entire meta node
	reqRateLimit   uint64
	reqRateLimiter = rate.NewLimiter(rate.Inf, DefaultReqLimitBurst)

	// map[opcode]*rate.Limiter, request rate limiter for opcode
	reqOpRateLimitMap   = make(map[uint8]uint64)
	reqOpRateLimiterMap = make(map[uint8]*rate.Limiter)
	reqVolOpPartRateLimitMap = make(map[string]map[uint8]uint64)
	reqVolOpPartRateLimiterMap = make(map[string]map[uint8]*rate.Limiter)

	isRateLimitOn bool

	// all cluster internal nodes
	clusterMap     = make(map[string]bool)
	limitInfo      *proto.LimitInfo
	limitOpcodeMap = map[uint8]bool{
		proto.OpMetaCreateInode:     true,
		proto.OpMetaInodeGet:        true,
		proto.OpMetaCreateDentry:    true,
		proto.OpMetaExtentsAdd:      true,
		proto.OpMetaBatchExtentsAdd: true,
		proto.OpMetaExtentsList:     true,
		proto.OpMetaInodeGetV2:      true,
		proto.OpMetaReadDir:         true,

		proto.OpMetaLookup:                    true,
		proto.OpMetaBatchInodeGet:             true,
		proto.OpMetaBatchGetXAttr:             true,
		proto.OpMetaBatchUnlinkInode:          true,
		proto.OpMetaBatchDeleteDentry:         true,
		proto.OpMetaBatchEvictInode:           true,
		proto.OpListMultiparts:                true,
		proto.OpMetaGetCmpInode:               true,
		proto.OpMetaInodeMergeEks:             true,
		proto.OpMetaGetDeletedInode:           true,
		proto.OpMetaBatchGetDeletedInode:      true,
		proto.OpMetaRecoverDeletedDentry:      true,
		proto.OpMetaBatchRecoverDeletedDentry: true,
		proto.OpMetaRecoverDeletedInode:       true,
		proto.OpMetaBatchRecoverDeletedInode:  true,
		proto.OpMetaBatchCleanDeletedDentry:   true,
		proto.OpMetaBatchCleanDeletedInode:    true,
	}

	RocksDBModeMaxFsUsedPercent  uint64 = DefaultRocksDBModeMaxFsUsedPercent
	MemModeMaxFsUsedPercent      uint64 = DefaultMemModeMaxFsUsedFactorPercent
)

func DeleteBatchCount() uint64 {
	val := atomic.LoadUint64(&nodeInfo.deleteBatchCount)
	if val == 0 {
		val = DefaultDeleteBatchCounts
	}
	return val
}

func updateDeleteBatchCount(val uint64) {
	atomic.StoreUint64(&nodeInfo.deleteBatchCount, val)
}

func ReadDirLimitNum() uint64 {
	val := atomic.LoadUint64(&nodeInfo.readDirLimitNum)
	return val
}

func updateReadDirLimitNum(val uint64)  {
	atomic.StoreUint64(&nodeInfo.readDirLimitNum, val)
}

func updateDeleteWorkerSleepMs(val uint64) {
	atomic.StoreUint64(&deleteWorkerSleepMs, val)
}

func DeleteWorkerSleepMs() {
	val := atomic.LoadUint64(&deleteWorkerSleepMs)
	if val > 0 {
		time.Sleep(time.Duration(val) * time.Millisecond)
	}
}

func GetDumpWaterLevel() uint64 {
	val := atomic.LoadUint64(&nodeInfo.dumpWaterLevel)
	if val < DefaultDumpWaterLevel {
		val = DefaultDumpWaterLevel
	}
	return val
}

func updateDumpWaterLevel(val uint64)  {
	atomic.StoreUint64(&nodeInfo.dumpWaterLevel, val)
}

func updateRocksDBModeMaxFsUsedPercent(val float32) {
	if val <= 0 || val >= 1 {
		return
	}
	atomic.StoreUint64(&RocksDBModeMaxFsUsedPercent, uint64(val * 100))
}

func getRocksDBModeMaxFsUsedPercent() uint64 {
	return atomic.LoadUint64(&RocksDBModeMaxFsUsedPercent)
}

func updateMemModeMaxFsUsedPercent(val float32) {
	if val <= 0 || val >= 1 {
		return
	}
	atomic.StoreUint64(&MemModeMaxFsUsedPercent, uint64(val * 100))
}

func getMemModeMaxFsUsedPercent() uint64 {
	return atomic.LoadUint64(&MemModeMaxFsUsedPercent)
}

func (m *MetaNode) startUpdateNodeInfo() {
	deleteTicker := time.NewTicker(UpdateDeleteLimitInfoTicket)
	rateLimitTicker := time.NewTicker(UpdateRateLimitInfoTicket)
	clusterViewTicker := time.NewTicker(UpdateClusterViewTicket)
	defer func() {
		deleteTicker.Stop()
		rateLimitTicker.Stop()
		clusterViewTicker.Stop()
	}()

	// call once on init before first tick
	m.updateClusterMap()
	m.updateDeleteLimitInfo()
	m.updateRateLimitInfo()
	for {
		select {
		case <-nodeInfoStopC:
			log.LogInfo("metanode nodeinfo gorutine stopped")
			return
		case <-deleteTicker.C:
			m.updateDeleteLimitInfo()
		case <-rateLimitTicker.C:
			m.updateRateLimitInfo()
		case <-clusterViewTicker.C:
			m.updateClusterMap()
		}
	}
}

func (m *MetaNode) stopUpdateNodeInfo() {
	nodeInfoStopC <- struct{}{}
}

func (m *MetaNode) updateDeleteLimitInfo() {
	info, err := masterClient.AdminAPI().GetLimitInfo("")
	if err != nil {
		log.LogErrorf("[updateDeleteLimitInfo] %s", err.Error())
		return
	}

	limitInfo = info
	updateDeleteBatchCount(limitInfo.MetaNodeDeleteBatchCount)
	updateDeleteWorkerSleepMs(limitInfo.MetaNodeDeleteWorkerSleepMs)
	updateReadDirLimitNum(limitInfo.MetaNodeReadDirLimitNum)
	updateDumpWaterLevel(limitInfo.MetaNodeDumpWaterLevel)
	updateMemModeMaxFsUsedPercent(limitInfo.MemModeRocksdbDiskUsageThreshold)
	updateRocksDBModeMaxFsUsedPercent(limitInfo.RocksdbDiskUsageThreshold)

	if statistics.StatisticsModule != nil {
		statistics.StatisticsModule.UpdateMonitorSummaryTime(limitInfo.MonitorSummarySec)
		statistics.StatisticsModule.UpdateMonitorReportTime(limitInfo.MonitorReportSec)
	}
}

func (m *MetaNode) updateTotReqLimitInfo(info *proto.LimitInfo) {
	reqRateLimit = info.MetaNodeReqRateLimit
	l := rate.Limit(reqRateLimit)
	if reqRateLimit == 0 {
		l = rate.Inf
	}
	reqRateLimiter.SetLimit(l)
}

func (m *MetaNode) updateOpLimitiInfo(info *proto.LimitInfo) {
	var (
		r                   uint64
		ok                  bool
		tmpOpRateLimiterMap map[uint8]*rate.Limiter
	)

	// update request rate limiter for opcode
	if reflect.DeepEqual(reqOpRateLimitMap, info.MetaNodeReqOpRateLimitMap) {
		return
	}
	reqOpRateLimitMap = info.MetaNodeReqOpRateLimitMap
	tmpOpRateLimiterMap = make(map[uint8]*rate.Limiter)
	for op, _ := range limitOpcodeMap {
		r, ok = reqOpRateLimitMap[op]
		if !ok {
			r, ok = reqOpRateLimitMap[0]
		}
		if !ok {
			continue
		}
		tmpOpRateLimiterMap[op] = rate.NewLimiter(rate.Limit(r), DefaultReqLimitBurst)
	}
	reqOpRateLimiterMap = tmpOpRateLimiterMap
}

func (m *MetaNode) updateVolLimitiInfo(info *proto.LimitInfo) {
	if reflect.DeepEqual(reqOpRateLimitMap, info.MetaNodeReqVolOpRateLimitMap) {
		return
	}
	reqVolOpPartRateLimitMap = info.MetaNodeReqVolOpRateLimitMap
	tmpVolOpRateLimiterMap := make(map[string]map[uint8]*rate.Limiter)
	for vol, volOps := range reqVolOpPartRateLimitMap {
		opRateLimiterMap := make(map[uint8]*rate.Limiter)
		tmpVolOpRateLimiterMap[vol] = opRateLimiterMap
		for op, opValue := range volOps {
			//op is not limit
			isLimitOp, ok := limitOpcodeMap[op]
			if !ok || !isLimitOp {
				continue
			}

			//set limit
			l := rate.Limit(opValue)
			opRateLimiterMap[op] = rate.NewLimiter(l, DefaultReqLimitBurst)
		}
	}
	reqVolOpPartRateLimiterMap = tmpVolOpRateLimiterMap
}

func (m *MetaNode) updateRateLimitInfo() {
	info := limitInfo
	if info == nil {
		return
	}

	m.updateTotReqLimitInfo(info)
	m.updateOpLimitiInfo(info)
	m.updateVolLimitiInfo(info)

	isRateLimitOn = (reqRateLimit > 0 ||
		len(reqOpRateLimitMap) > 0 ||
		len(reqVolOpPartRateLimitMap) > 0 )
}

func (m *MetaNode) updateClusterMap() {
	cv, err := masterClient.AdminAPI().GetCluster()
	if err != nil {
		return
	}
	addrMap := make(map[string]bool, len(clusterMap))
	var addrSlice []string
	for _, node := range cv.MetaNodes {
		addrSlice = strings.Split(node.Addr, ":")
		addrMap[addrSlice[0]] = true
	}
	for _, node := range cv.DataNodes {
		addrSlice = strings.Split(node.Addr, ":")
		addrMap[addrSlice[0]] = true
	}
	for _, master := range masterClient.Nodes() {
		addrSlice = strings.Split(master, ":")
		addrMap[addrSlice[0]] = true
	}
	clusterMap = addrMap
}
