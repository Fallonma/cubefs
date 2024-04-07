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

package datanode

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"os"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cubefs/cubefs/util/topology"

	"github.com/cubefs/cubefs/datanode/riskdata"
	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/raftstore"
	"github.com/cubefs/cubefs/repl"
	"github.com/cubefs/cubefs/storage"
	"github.com/cubefs/cubefs/util/errors"
	"github.com/cubefs/cubefs/util/exporter"
	"github.com/cubefs/cubefs/util/holder"
	"github.com/cubefs/cubefs/util/log"
	"github.com/cubefs/cubefs/util/multirate"
	"github.com/cubefs/cubefs/util/statistics"
	"github.com/cubefs/cubefs/util/unit"
	"github.com/tiglabs/raft"
	raftProto "github.com/tiglabs/raft/proto"
	"github.com/tiglabs/raft/storage/wal"
)

const (
	DataPartitionPrefix           = "datapartition"
	DataPartitionMetadataFileName = "META"
	TempMetadataFileName          = ".meta"
	ApplyIndexFile                = "APPLY"
	TempApplyIndexFile            = ".apply"
	TimeLayout                    = "2006-01-02 15:04:05"
)

type FaultOccurredCheckLevel uint8

const (
	CheckNothing FaultOccurredCheckLevel = iota // default value, no need fault occurred check or check finished
	// CheckQuorumCommitID never persist
	CheckQuorumCommitID // fetch commit with quorum in fault occurred check
	CheckAllCommitID    // fetch commit with all in fault occurred check
)

type DataPartitionMetadata struct {
	VolumeID                string
	PartitionID             uint64
	PartitionSize           int
	CreateTime              string
	Peers                   []proto.Peer
	Hosts                   []string
	Learners                []proto.Learner
	ReplicaNum              int
	DataPartitionCreateType int
	LastTruncateID          uint64
	VolumeHAType            proto.CrossRegionHAType
	ConsistencyMode         proto.ConsistencyMode

	// 该BOOL值表示Partition是否已经就绪，该值默认值为false，
	// 新创建的DP成员为默认值，表示未完成第一次Raft恢复，Raft未就绪。
	// 当第一次快照或者有应用日志行为时，该值被置为true并需要持久化该信息。
	// 当发生快照应用(Apply Snapshot)行为时，该值为true。该DP需要关闭并进行报警。
	IsCatchUp            bool
	NeedServerFaultCheck bool
}

func (md *DataPartitionMetadata) Equals(other *DataPartitionMetadata) bool {
	return (md == nil && other == nil) ||
		(md != nil && other != nil && md.VolumeID == other.VolumeID &&
			md.PartitionID == other.PartitionID &&
			md.PartitionSize == other.PartitionSize &&
			reflect.DeepEqual(md.Peers, other.Peers) &&
			reflect.DeepEqual(md.Hosts, other.Hosts) &&
			reflect.DeepEqual(md.Learners, other.Learners) &&
			md.ReplicaNum == other.ReplicaNum &&
			md.DataPartitionCreateType == other.DataPartitionCreateType &&
			md.LastTruncateID == other.LastTruncateID &&
			md.VolumeHAType == other.VolumeHAType) &&
			md.IsCatchUp == other.IsCatchUp &&
			md.NeedServerFaultCheck == other.NeedServerFaultCheck &&
			md.ConsistencyMode == other.ConsistencyMode
}

func (md *DataPartitionMetadata) Validate() (err error) {
	md.VolumeID = strings.TrimSpace(md.VolumeID)
	if len(md.VolumeID) == 0 || md.PartitionID == 0 || md.PartitionSize == 0 {
		err = errors.New("illegal data partition metadata")
		return
	}
	return
}

type sortedPeers []proto.Peer

func (sp sortedPeers) Len() int {
	return len(sp)
}

func (sp sortedPeers) Less(i, j int) bool {
	return sp[i].ID < sp[j].ID
}

func (sp sortedPeers) Swap(i, j int) {
	sp[i], sp[j] = sp[j], sp[i]
}

type WALApplyStatus struct {
	applied   uint64
	truncated uint64

	mu sync.RWMutex
}

func (s *WALApplyStatus) Init(applied, truncated uint64) (success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if applied == 0 || (applied != 0 && applied >= truncated) {
		s.applied, s.truncated = applied, truncated
		success = true
	}
	return
}

func (s *WALApplyStatus) AdvanceApplied(id uint64) (snap WALApplyStatus, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied < id && s.truncated <= id {
		s.applied = id
		success = true
	}
	snap = WALApplyStatus{
		applied:   s.applied,
		truncated: s.truncated,
	}
	return
}

func (s *WALApplyStatus) Applied() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.applied
}

func (s *WALApplyStatus) AdvanceTruncated(id uint64) (snap WALApplyStatus, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.truncated < id && id <= s.applied {
		s.truncated = id
		success = true
	}
	snap = WALApplyStatus{
		applied:   s.applied,
		truncated: s.truncated,
	}
	return
}

func (s *WALApplyStatus) Truncated() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.truncated
}

func (s *WALApplyStatus) Snap() *WALApplyStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &WALApplyStatus{
		applied:   s.applied,
		truncated: s.truncated,
	}
}

func NewWALApplyStatus() *WALApplyStatus {
	return &WALApplyStatus{}
}

type DataPartition struct {
	clusterID       string
	volumeID        string
	partitionID     uint64
	partitionStatus int
	partitionSize   int
	replicas        []string // addresses of the replicas
	replicasLock    sync.RWMutex
	disk            *Disk
	isReplLeader    bool
	isRaftLeader    bool
	path            string
	used            int
	extentStore     *storage.ExtentStore
	raftPartition   raftstore.Partition
	config          *dataPartitionCfg

	isCatchUp             bool
	needServerFaultCheck  bool
	serverFaultCheckLevel FaultOccurredCheckLevel
	applyStatus           *WALApplyStatus

	repairPropC              chan struct{}
	updateVolInfoPropC       chan struct{}
	latestPropUpdateReplicas int64 // 记录最近一次申请更新Replicas信息的时间戳，单位为秒

	stopOnce  sync.Once
	stopRaftC chan uint64
	stopC     chan bool

	intervalToUpdateReplicas      int64 // interval to ask the master for updating the replica information
	snapshot                      []*proto.File
	snapshotMutex                 sync.RWMutex
	intervalToUpdatePartitionSize int64
	loadExtentHeaderStatus        int
	FullSyncTinyDeleteTime        int64
	lastSyncTinyDeleteTime        int64
	DataPartitionCreateType       int
	monitorData                   []*statistics.MonitorData
	topologyManager               *topology.TopologyManager
	persistSync                   chan struct{}

	inRepairExtents  map[uint64]struct{}
	inRepairExtentMu sync.Mutex

	persistedApplied  uint64
	persistedMetadata *DataPartitionMetadata

	actionHolder *holder.ActionHolder
	dataFixer    *riskdata.Fixer
}

type DataPartitionViewInfo struct {
	VolName              string                    `json:"volName"`
	ID                   uint64                    `json:"id"`
	Size                 int                       `json:"size"`
	Used                 int                       `json:"used"`
	Status               int                       `json:"status"`
	Path                 string                    `json:"path"`
	Files                []storage.ExtentInfoBlock `json:"extents"`
	FileCount            int                       `json:"fileCount"`
	Replicas             []string                  `json:"replicas"`
	TinyDeleteRecordSize int64                     `json:"tinyDeleteRecordSize"`
	RaftStatus           *raft.Status              `json:"raftStatus"`
	Peers                []proto.Peer              `json:"peers"`
	Learners             []proto.Learner           `json:"learners"`
	IsFinishLoad         bool                      `json:"isFinishLoad"`
	IsRecover            bool                      `json:"isRecover"`
	BaseExtentID         uint64                    `json:"baseExtentID"`
	RiskFixerStatus      *riskdata.FixerStatus     `json:"riskFixerStatus"`
}

func (d *Disk) createPartition(dpCfg *dataPartitionCfg, request *proto.CreateDataPartitionRequest) (dp *DataPartition, err error) {

	if dp, err = newDataPartition(dpCfg, d, true, d.topoManager, d.interceptors); err != nil {
		return
	}
	dp.ForceLoadHeader()

	// persist file metadata
	dp.DataPartitionCreateType = request.CreateType
	err = dp.persistMetaDataOnly()
	d.AddSize(uint64(dp.Size()))
	if err = dp.initIssueProcessor(0); err != nil {
		return
	}
	return
}

func (dp *DataPartition) ID() uint64 {
	return dp.partitionID
}

func (dp *DataPartition) AllocateExtentID() (id uint64, err error) {
	id, err = dp.extentStore.NextExtentID()
	return
}

func (dp *DataPartition) IsEquareCreateDataPartitionRequst(request *proto.CreateDataPartitionRequest) (err error) {
	if len(dp.config.Peers) != len(request.Members) {
		return fmt.Errorf("Exsit unavali Partition(%v) partitionHosts(%v) requestHosts(%v)", dp.partitionID, dp.config.Peers, request.Members)
	}
	for index, host := range dp.config.Hosts {
		requestHost := request.Hosts[index]
		if host != requestHost {
			return fmt.Errorf("Exsit unavali Partition(%v) partitionHosts(%v) requestHosts(%v)", dp.partitionID, dp.config.Hosts, request.Hosts)
		}
	}
	sp := sortedPeers(dp.config.Peers)
	sort.Sort(sp)
	requestSP := sortedPeers(request.Members)
	sort.Sort(requestSP)
	for index, peer := range dp.config.Peers {
		requestPeer := request.Members[index]
		if requestPeer.ID != peer.ID || requestPeer.Addr != peer.Addr {
			return fmt.Errorf("Exist unavali Partition(%v) partitionHosts(%v) requestHosts(%v)", dp.partitionID, dp.config.Peers, request.Members)
		}
	}
	for index, learner := range dp.config.Learners {
		requestLearner := request.Learners[index]
		if requestLearner.ID != learner.ID || requestLearner.Addr != learner.Addr {
			return fmt.Errorf("Exist unavali Partition(%v) partitionLearners(%v) requestLearners(%v)", dp.partitionID, dp.config.Learners, request.Learners)
		}
	}
	if dp.config.VolName != request.VolumeId {
		return fmt.Errorf("Exist unavali Partition(%v) VolName(%v) requestVolName(%v)", dp.partitionID, dp.config.VolName, request.VolumeId)
	}
	return
}

// LoadDataPartition loads and returns a partition instance based on the specified directory.
// It reads the partition metadata file stored under the specified directory
// and creates the partition instance.
func (d *Disk) loadPartition(partitionDir string) (dp *DataPartition, err error) {
	var (
		metaFileData []byte
	)
	if metaFileData, err = ioutil.ReadFile(path.Join(partitionDir, DataPartitionMetadataFileName)); err != nil {
		return
	}
	meta := &DataPartitionMetadata{}
	if err = json.Unmarshal(metaFileData, meta); err != nil {
		return
	}
	if err = meta.Validate(); err != nil {
		return
	}

	dpCfg := &dataPartitionCfg{
		VolName:       meta.VolumeID,
		PartitionSize: meta.PartitionSize,
		PartitionID:   meta.PartitionID,
		ReplicaNum:    meta.ReplicaNum,
		Peers:         meta.Peers,
		Hosts:         meta.Hosts,
		Learners:      meta.Learners,
		RaftStore:     d.space.GetRaftStore(),
		NodeID:        d.space.GetNodeID(),
		ClusterID:     d.space.GetClusterID(),
		CreationType:  meta.DataPartitionCreateType,

		VolHAType: meta.VolumeHAType,
		Mode:      meta.ConsistencyMode,
	}
	if dp, err = newDataPartition(dpCfg, d, false, d.topoManager, d.interceptors); err != nil {
		return
	}
	// dp.PersistMetadata()

	var appliedID uint64
	if appliedID, err = dp.LoadAppliedID(); err != nil {
		log.LogErrorf("action[loadApplyIndex] %v", err)
	}
	log.LogInfof("Action(LoadDataPartition) PartitionID(%v) meta(%v)", dp.partitionID, meta)
	dp.DataPartitionCreateType = meta.DataPartitionCreateType
	dp.isCatchUp = meta.IsCatchUp
	dp.needServerFaultCheck = meta.NeedServerFaultCheck
	dp.serverFaultCheckLevel = CheckAllCommitID

	if !dp.applyStatus.Init(appliedID, meta.LastTruncateID) {
		err = fmt.Errorf("action[loadApplyIndex] illegal metadata, appliedID %v, lastTruncateID %v", appliedID, meta.LastTruncateID)
		return
	}

	d.AddSize(uint64(dp.Size()))
	dp.ForceLoadHeader()

	// 检查是否有需要更新Volume信息
	var maybeNeedUpdateCrossRegionHAType = func() bool {
		return (len(dp.config.Hosts) > 3 && dp.config.VolHAType == proto.DefaultCrossRegionHAType) ||
			(len(dp.config.Hosts) <= 3 && dp.config.VolHAType == proto.CrossRegionHATypeQuorum)
	}
	var maybeNeedUpdateReplicaNum = func() bool {
		return dp.config.ReplicaNum == 0 || len(dp.config.Hosts) != dp.config.ReplicaNum
	}
	if maybeNeedUpdateCrossRegionHAType() || maybeNeedUpdateReplicaNum() {
		dp.proposeUpdateVolumeInfo()
	}

	dp.persistedApplied = appliedID
	dp.persistedMetadata = meta
	dp.maybeUpdateFaultOccurredCheckLevel()
	if err = dp.initIssueProcessor(d.latestFlushTimeOnInit); err != nil {
		return
	}
	return
}

func (dp *DataPartition) initIssueProcessor(latestFlushTimeUnix int64) (err error) {
	var fragments []*riskdata.Fragment
	if dp.needServerFaultCheck {
		if fragments, err = dp.scanIssueFragments(latestFlushTimeUnix); err != nil {
			return
		}
	}
	var getRemotes riskdata.GetRemotesFunc = func() []string {
		var replicas = dp.getReplicaClone()
		var remotes = make([]string, 0, len(replicas)-1)
		for _, replica := range replicas {
			if !dp.IsLocalAddress(replica) {
				remotes = append(remotes, replica)
			}
		}
		return remotes
	}
	var getHAType riskdata.GetHATypeFunc = func() proto.CrossRegionHAType {
		return dp.config.VolHAType
	}
	if dp.dataFixer, err = riskdata.NewFixer(dp.partitionID, dp.path, dp.extentStore, getRemotes, getHAType, fragments, gConnPool, dp.disk.Path, dp.limit); err != nil {
		return
	}
	return
}

func (dp *DataPartition) CheckRisk(extentID, offset, size uint64) bool {
	return dp.dataFixer.FindOverlap(extentID, offset, size)
}

func (dp *DataPartition) maybeUpdateFaultOccurredCheckLevel() {
	if maybeServerFaultOccurred {
		dp.setNeedFaultCheck(true)
		_ = dp.persistMetaDataOnly()
	}
}

func newDataPartition(dpCfg *dataPartitionCfg, disk *Disk, isCreatePartition bool, fetchtopoManager *topology.TopologyManager, interceptors storage.IOInterceptors) (dp *DataPartition, err error) {
	partitionID := dpCfg.PartitionID
	dataPath := path.Join(disk.Path, fmt.Sprintf(DataPartitionPrefix+"_%v_%v", partitionID, dpCfg.PartitionSize))
	partition := &DataPartition{
		volumeID:                dpCfg.VolName,
		clusterID:               dpCfg.ClusterID,
		partitionID:             partitionID,
		disk:                    disk,
		path:                    dataPath,
		partitionSize:           dpCfg.PartitionSize,
		replicas:                make([]string, 0),
		repairPropC:             make(chan struct{}, 1),
		updateVolInfoPropC:      make(chan struct{}, 1),
		stopC:                   make(chan bool, 0),
		stopRaftC:               make(chan uint64, 0),
		snapshot:                make([]*proto.File, 0),
		partitionStatus:         proto.ReadWrite,
		config:                  dpCfg,
		DataPartitionCreateType: dpCfg.CreationType,
		monitorData:             statistics.InitMonitorData(statistics.ModelDataNode),
		persistSync:             make(chan struct{}, 1),
		inRepairExtents:         make(map[uint64]struct{}),
		topologyManager:         fetchtopoManager,
		applyStatus:             NewWALApplyStatus(),
		actionHolder:            holder.NewActionHolder(),
	}
	partition.replicasInit()

	var cacheListener storage.CacheListener = func(event storage.CacheEvent, e *storage.Extent) {
		switch event {
		case storage.CacheEvent_Add:
			disk.IncreaseFDCount()
		case storage.CacheEvent_Evict:
			disk.DecreaseFDCount()
		}
	}

	partition.extentStore, err = storage.NewExtentStore(partition.path, dpCfg.PartitionID, dpCfg.PartitionSize, CacheCapacityPerPartition, cacheListener, isCreatePartition, interceptors)
	if err != nil {
		return
	}

	rand.Seed(time.Now().UnixNano())
	partition.FullSyncTinyDeleteTime = time.Now().Unix() + rand.Int63n(3600*24)
	partition.lastSyncTinyDeleteTime = partition.FullSyncTinyDeleteTime
	dp = partition
	return
}

func (dp *DataPartition) RaftStatus() *raftstore.PartitionStatus {
	if dp.raftPartition != nil {
		return dp.raftPartition.Status()
	}
	return nil
}

func (dp *DataPartition) RaftHardState() (hs raftProto.HardState, err error) {
	hs, err = dp.tryLoadRaftHardStateFromDisk()
	return
}

func (dp *DataPartition) tryLoadRaftHardStateFromDisk() (hs raftProto.HardState, err error) {
	var walPath = path.Join(dp.path, "wal_"+strconv.FormatUint(dp.partitionID, 10))
	var metaFile *wal.MetaFile
	if metaFile, hs, _, err = wal.OpenMetaFile(walPath); err != nil {
		return
	}
	_ = metaFile.Close()
	return
}

func (dp *DataPartition) Start() (err error) {
	go func() {
		go dp.statusUpdateScheduler(context.Background())
		if dp.DataPartitionCreateType == proto.DecommissionedCreateDataPartition {
			dp.startRaftAfterRepair()
			return
		}
		dp.startRaftAsync()
	}()
	return
}

func (dp *DataPartition) RiskFixer() *riskdata.Fixer {
	return dp.dataFixer
}

func (dp *DataPartition) replicasInit() {
	replicas := make([]string, 0)
	if dp.config.Hosts == nil {
		return
	}
	for _, host := range dp.config.Hosts {
		replicas = append(replicas, host)
	}
	dp.replicasLock.Lock()
	dp.replicas = replicas
	dp.replicasLock.Unlock()
	if dp.config.Hosts != nil && len(dp.config.Hosts) >= 1 {
		leaderAddr := strings.Split(dp.config.Hosts[0], ":")
		if len(leaderAddr) == 2 && strings.TrimSpace(leaderAddr[0]) == LocalIP {
			dp.isReplLeader = true
		}
	}
}

func (dp *DataPartition) GetExtentCount() int {
	return dp.extentStore.GetExtentCount()
}

func (dp *DataPartition) Path() string {
	return dp.path
}

// IsRaftLeader tells if the given address belongs to the raft leader.
func (dp *DataPartition) IsRaftLeader() (addr string, ok bool) {
	if dp.raftPartition == nil {
		return
	}
	leaderID, _ := dp.raftPartition.LeaderTerm()
	if leaderID == 0 {
		return
	}
	ok = leaderID == dp.config.NodeID
	for _, peer := range dp.config.Peers {
		if leaderID == peer.ID {
			addr = peer.Addr
			return
		}
	}
	return
}

func (dp *DataPartition) IsRaftStarted() bool {
	return dp.raftPartition != nil
}

func (dp *DataPartition) IsLocalAddress(addr string) bool {
	var addrID uint64
	if dp.config == nil {
		return false
	}
	for _, peer := range dp.config.Peers {
		if addr == peer.Addr {
			addrID = peer.ID
			break
		}
	}
	if addrID == dp.config.NodeID {
		return true
	}
	return false
}

func (dp *DataPartition) IsRandomWriteDisabled() (disabled bool) {
	disabled = dp.config.VolHAType == proto.CrossRegionHATypeQuorum
	return
}

func (dp *DataPartition) IsRaftLearner() bool {
	for _, learner := range dp.config.Learners {
		if learner.ID == dp.config.NodeID {
			return true
		}
	}
	return false
}

func (dp *DataPartition) getReplicaClone() (newReplicas []string) {
	dp.replicasLock.RLock()
	defer dp.replicasLock.RUnlock()
	newReplicas = make([]string, len(dp.replicas))
	copy(newReplicas, dp.replicas)
	return
}

func (dp *DataPartition) IsExistReplica(addr string) bool {
	dp.replicasLock.RLock()
	defer dp.replicasLock.RUnlock()
	for _, host := range dp.replicas {
		if host == addr {
			return true
		}
	}
	return false
}

func (dp *DataPartition) IsExistLearner(tarLearner proto.Learner) bool {
	dp.replicasLock.RLock()
	defer dp.replicasLock.RUnlock()
	for _, learner := range dp.config.Learners {
		if learner.Addr == tarLearner.Addr && learner.ID == tarLearner.ID {
			return true
		}
	}
	return false
}

func (dp *DataPartition) ReloadSnapshot() {
	files, err := dp.extentStore.SnapShot()
	if err != nil {
		return
	}
	dp.snapshotMutex.Lock()
	for _, f := range dp.snapshot {
		storage.PutSnapShotFileToPool(f)
	}
	dp.snapshot = files
	dp.snapshotMutex.Unlock()
}

// Snapshot returns the snapshot of the data partition.
func (dp *DataPartition) SnapShot() (files []*proto.File) {
	dp.snapshotMutex.RLock()
	defer dp.snapshotMutex.RUnlock()

	return dp.snapshot
}

// Stop close the store and the raft store.
func (dp *DataPartition) Stop() {
	dp.stopOnce.Do(func() {
		if dp.stopC != nil {
			close(dp.stopC)
		}
		// Close the store and raftstore.
		dp.dataFixer.Stop()
		dp.extentStore.Close()
		dp.stopRaft()
		if err := dp.persist(nil, false); err != nil {
			log.LogErrorf("persist partition [%v] failed when stop: %v", dp.partitionID, err)
		}
	})
	return
}

func (dp *DataPartition) Delete() {
	if dp == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			mesg := fmt.Sprintf("DataPartition(%v) Delete panic(%v)", dp.partitionID, r)
			log.LogWarnf(mesg)
		}
	}()
	dp.Stop()
	dp.Disk().DetachDataPartition(dp)
	if dp.raftPartition != nil {
		_ = dp.raftPartition.Delete()
	} else {
		log.LogWarnf("action[Delete] raft instance not ready! dp:%v", dp.config.PartitionID)
	}
	_ = os.RemoveAll(dp.Path())
}

func (dp *DataPartition) MarkDelete(marker storage.Marker) (err error) {
	err = dp.extentStore.MarkDelete(marker)
	return
}

func (dp *DataPartition) FlushDelete(limit int) (deleted, remain int, err error) {

	const (
		exporterOp            = "FlushDelete"
		ctxKeyExporterTp byte = 0x0
		ctxKeySreTp      byte = 0x01
	)

	var (
		monitorData                    = dp.monitorData[proto.ActionFlushDelete]
		before      storage.BeforeFunc = func() (ctx context.Context, err error) {
			ctx = context.Background()
			err = dp.limit(ctx, proto.OpFlushDelete_, 0, multirate.FlowDisk)
			if err != nil {
				return
			}
			ctx = context.WithValue(ctx, ctxKeyExporterTp, exporter.NewModuleTP(exporterOp))
			ctx = context.WithValue(ctx, ctxKeySreTp, monitorData.BeforeTp())
			return
		}
		after storage.AfterFunc = func(ctx context.Context, n int64, err error) {
			if tp, is := ctx.Value(ctxKeyExporterTp).(exporter.TP); is {
				tp.Set(err)
			}
			if tp, is := ctx.Value(ctxKeySreTp).(*statistics.TpObject); is {
				tp.AfterTp(0)
			}
		}
	)
	deleted, remain, err = dp.extentStore.FlushDelete(storage.NewFuncInterceptor(before, after), limit)
	return
}

func (dp *DataPartition) Expired() {
	if dp == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			mesg := fmt.Sprintf("DataPartition(%v) Expired panic(%v)", dp.partitionID, r)
			log.LogWarnf(mesg)
		}
	}()

	dp.Stop()
	dp.Disk().DetachDataPartition(dp)
	var currentPath = path.Clean(dp.path)
	var newPath = path.Join(path.Dir(currentPath),
		ExpiredPartitionPrefix+path.Base(currentPath)+"_"+strconv.FormatInt(time.Now().Unix(), 10))
	if err := os.Rename(currentPath, newPath); err != nil {
		log.LogErrorf("ExpiredPartition: mark expired partition fail: volume(%v) partitionID(%v) path(%v) newPath(%v) err(%v)",
			dp.volumeID,
			dp.partitionID,
			dp.path,
			newPath,
			err)
		return
	}
	log.LogInfof("ExpiredPartition: mark expired partition: volume(%v) partitionID(%v) path(%v) newPath(%v)",
		dp.volumeID,
		dp.partitionID,
		dp.path,
		newPath)
}

// Disk returns the disk instance.
func (dp *DataPartition) Disk() *Disk {
	return dp.disk
}

func (dp *DataPartition) CheckWritable() error {
	if dp.Disk().Status == proto.Unavailable {
		return storage.BrokenDiskError
	}
	if dp.used > dp.partitionSize*2 || dp.Disk().Status == proto.ReadOnly {
		return storage.NoSpaceError
	}
	return nil
}

const (
	MinDiskSpace = 10 * 1024 * 1024 * 1024
)

func (dp *DataPartition) IsRejectRandomWrite() bool {
	return dp.Disk().Available < MinDiskSpace
}

// Status returns the partition status.
func (dp *DataPartition) Status() int {
	return dp.partitionStatus
}

// Size returns the partition size.
func (dp *DataPartition) Size() int {
	return dp.partitionSize
}

// Used returns the used space.
func (dp *DataPartition) Used() int {
	return dp.used
}

// Available returns the available space.
func (dp *DataPartition) Available() int {
	return dp.partitionSize - dp.used
}

func (dp *DataPartition) ForceLoadHeader() {
	dp.loadExtentHeaderStatus = FinishLoadDataPartitionExtentHeader
}

func (dp *DataPartition) proposeRepair() {
	select {
	case dp.repairPropC <- struct{}{}:
	default:
	}
}

func (dp *DataPartition) proposeUpdateVolumeInfo() {
	select {
	case dp.updateVolInfoPropC <- struct{}{}:
	default:
	}
}

func (dp *DataPartition) statusUpdateScheduler(ctx context.Context) {
	rand := rand.New(rand.NewSource(time.Now().UnixNano()))
	repairTimer := time.NewTimer(time.Minute + time.Duration(rand.Intn(120))*time.Second)
	validateCRCTimer := time.NewTimer(DefaultIntervalDataPartitionValidateCRC)
	retryUpdateVolInfoTimer := time.NewTimer(0)
	retryUpdateVolInfoTimer.Stop()
	persistDpLastUpdateTimer := time.NewTimer(time.Hour) //for persist dp lastUpdateTime
	var index int
	for {

		select {
		case <-dp.stopC:
			repairTimer.Stop()
			validateCRCTimer.Stop()
			return

		case <-dp.repairPropC:
			repairTimer.Stop()
			log.LogDebugf("partition(%v) execute manual data repair for all extent", dp.partitionID)
			dp.ExtentStore().MoveAllToBrokenTinyExtentC(proto.TinyExtentCount)
			dp.runRepair(ctx, proto.TinyExtentType)
			dp.runRepair(ctx, proto.NormalExtentType)
			repairTimer.Reset(time.Minute)
		case <-repairTimer.C:
			index++
			dp.statusUpdate()
			if index >= math.MaxUint32 {
				index = 0
			}
			if err := dp.updateReplicas(); err != nil {
				log.LogWarnf("DP[%v] update replicas failed: %v", dp.partitionID, err)
				repairTimer.Reset(time.Minute)
				continue
			}
			if index%2 == 0 {
				dp.runRepair(ctx, proto.TinyExtentType)
			} else {
				dp.runRepair(ctx, proto.NormalExtentType)
			}
			repairTimer.Reset(time.Minute)
		case <-validateCRCTimer.C:
			dp.runValidateCRC(ctx)
			validateCRCTimer.Reset(DefaultIntervalDataPartitionValidateCRC)
		case <-dp.updateVolInfoPropC:
			if err := dp.updateVolumeInfoFromMaster(); err != nil {
				retryUpdateVolInfoTimer.Reset(time.Minute)
			}
		case <-retryUpdateVolInfoTimer.C:
			if err := dp.updateVolumeInfoFromMaster(); err != nil {
				retryUpdateVolInfoTimer.Reset(time.Minute)
			}
		case <-persistDpLastUpdateTimer.C:
			_ = dp.persistMetaDataOnly()
			persistDpLastUpdateTimer.Reset(time.Hour)
		}
	}
}

func (dp *DataPartition) updateVolumeInfoFromMaster() (err error) {
	var simpleVolView *proto.SimpleVolView
	if simpleVolView, err = MasterClient.AdminAPI().GetVolumeSimpleInfo(dp.volumeID); err != nil {
		return
	}
	// Process CrossRegionHAType
	var changed bool
	if dp.config.VolHAType != simpleVolView.CrossRegionHAType {
		dp.config.VolHAType = simpleVolView.CrossRegionHAType
		changed = true
	}
	if dp.config.ReplicaNum != int(simpleVolView.DpReplicaNum) {
		dp.config.ReplicaNum = int(simpleVolView.DpReplicaNum)
		changed = true
	}
	if changed {
		if err = dp.persistMetaDataOnly(); err != nil {
			return
		}
	}
	return
}

func (dp *DataPartition) statusUpdate() {
	status := proto.ReadWrite
	dp.computeUsage()

	if dp.used >= dp.partitionSize {
		status = proto.ReadOnly
	}
	if dp.extentStore.GetExtentCount() >= storage.MaxExtentCount {
		status = proto.ReadOnly
	}
	if dp.Status() == proto.Unavailable {
		status = proto.Unavailable
	}

	dp.partitionStatus = int(math.Min(float64(status), float64(dp.disk.Status)))
}

func (dp *DataPartition) computeUsage() {
	if time.Now().Unix()-dp.intervalToUpdatePartitionSize < IntervalToUpdatePartitionSize {
		return
	}
	dp.used = int(dp.ExtentStore().GetStoreUsedSize())
	dp.intervalToUpdatePartitionSize = time.Now().Unix()
}

func (dp *DataPartition) ExtentStore() *storage.ExtentStore {
	return dp.extentStore
}

func (dp *DataPartition) checkIsDiskError(err error) (diskError bool) {
	if err == nil {
		return
	}
	if IsDiskErr(err) {
		mesg := fmt.Sprintf("disk path %v error on %v", dp.Path(), LocalIP)
		exporter.Warning(mesg)
		log.LogErrorf(mesg)
		dp.stopRaft()
		dp.disk.incReadErrCnt()
		dp.disk.incWriteErrCnt()
		dp.disk.Status = proto.Unavailable
		dp.statusUpdate()
		dp.disk.ForceExitRaftStore()
		diskError = true
	}
	return
}

// String returns the string format of the data partition information.
func (dp *DataPartition) String() (m string) {
	return fmt.Sprintf(DataPartitionPrefix+"_%v_%v", dp.partitionID, dp.partitionSize)
}

// runRepair launches the repair of extents.
func (dp *DataPartition) runRepair(ctx context.Context, extentType uint8) {

	/*	if dp.partitionStatus == proto.Unavailable {
		return
	}*/

	if !dp.isReplLeader {
		return
	}
	if dp.extentStore.BrokenTinyExtentCnt() == 0 {
		dp.extentStore.MoveAllToBrokenTinyExtentC(MinTinyExtentsToRepair)
	}
	dp.repair(ctx, extentType)
}

func (dp *DataPartition) updateReplicas() (err error) {
	var isLeader bool
	replicas := make([]string, 0)
	dp.backendRefreshCacheView()
	partition, err := dp.getCacheView()
	if err != nil {
		return
	}
	for _, host := range partition.Hosts {
		replicas = append(replicas, host)
	}
	if partition.Hosts != nil && len(partition.Hosts) >= 1 {
		leaderAddr := strings.Split(partition.Hosts[0], ":")
		if len(leaderAddr) == 2 && strings.TrimSpace(leaderAddr[0]) == LocalIP {
			isLeader = true
		}
	}
	dp.replicasLock.Lock()
	defer dp.replicasLock.Unlock()
	if !dp.compareReplicas(dp.replicas, replicas) {
		log.LogInfof("action[updateReplicas] partition(%v) replicas changed from(%v) to(%v).",
			dp.partitionID, dp.replicas, replicas)
	}
	dp.isReplLeader = isLeader
	dp.replicas = replicas
	dp.intervalToUpdateReplicas = time.Now().Unix()
	log.LogInfof(fmt.Sprintf("ActionUpdateReplicationHosts partiton(%v)", dp.partitionID))

	return
}

// Compare the fetched replica with the local one.
func (dp *DataPartition) compareReplicas(v1, v2 []string) (equals bool) {
	equals = true
	if len(v1) == len(v2) {
		for i := 0; i < len(v1); i++ {
			if v1[i] != v2[i] {
				equals = false
				return
			}
		}
		equals = true
		return
	}
	equals = false
	return
}

func (dp *DataPartition) Load() (response *proto.LoadDataPartitionResponse) {
	response = &proto.LoadDataPartitionResponse{}
	response.PartitionId = uint64(dp.partitionID)
	response.PartitionStatus = dp.partitionStatus
	response.Used = uint64(dp.Used())

	if dp.loadExtentHeaderStatus != FinishLoadDataPartitionExtentHeader {
		response.PartitionSnapshot = make([]*proto.File, 0)
	} else {
		response.PartitionSnapshot = dp.SnapShot()
	}
	return
}

type TinyDeleteRecord struct {
	extentID uint64
	offset   uint64
	size     uint64
}

type TinyDeleteRecordArr []TinyDeleteRecord

func (dp *DataPartition) doStreamFixTinyDeleteRecord(ctx context.Context, repairTask *DataPartitionRepairTask, isFullSync bool) {
	var (
		originLocalTinyDeleteSize int64
		localTinyDeleteFileSize   int64
		err                       error
		conn                      *net.TCPConn
		isRealSync                bool
	)

	if !dp.Disk().canFinTinyDeleteRecord() {
		return
	}
	defer func() {
		dp.Disk().finishFixTinyDeleteRecord()
	}()
	log.LogInfof(ActionSyncTinyDeleteRecord+" start PartitionID(%v) localTinyDeleteFileSize(%v) leaderTinyDeleteFileSize(%v) "+
		"leaderAddr(%v) ,lastSyncTinyDeleteTime(%v) currentTime(%v) fullSyncTinyDeleteTime(%v) isFullSync(%v)",
		dp.partitionID, localTinyDeleteFileSize, repairTask.LeaderTinyDeleteRecordFileSize, repairTask.LeaderAddr,
		dp.lastSyncTinyDeleteTime, time.Now().Unix(), dp.FullSyncTinyDeleteTime, isFullSync)

	defer func() {
		log.LogInfof(ActionSyncTinyDeleteRecord+" end PartitionID(%v) originLocalTinyDeleteSize(%v) localTinyDeleteFileSize(%v) leaderTinyDeleteFileSize(%v) leaderAddr(%v) "+
			"err(%v), lastSyncTinyDeleteTime(%v) currentTime(%v) fullSyncTinyDeleteTime(%v) isFullSync(%v) isRealSync(%v)\",",
			dp.partitionID, originLocalTinyDeleteSize, localTinyDeleteFileSize, repairTask.LeaderTinyDeleteRecordFileSize, repairTask.LeaderAddr, err,
			dp.lastSyncTinyDeleteTime, time.Now().Unix(), dp.FullSyncTinyDeleteTime, isFullSync, isRealSync)
	}()
	if dp.DataPartitionCreateType != proto.DecommissionedCreateDataPartition && !isFullSync && time.Now().Unix()-dp.lastSyncTinyDeleteTime < MinSyncTinyDeleteTime {
		return
	}
	var release = dp.extentStore.LockFlushDelete()
	defer release()

	if isFullSync {
		dp.FullSyncTinyDeleteTime = time.Now().Unix()
		err = dp.extentStore.DropTinyDeleteRecord()
		if err != nil {
			return
		}
	}
	if localTinyDeleteFileSize, err = dp.extentStore.LoadTinyDeleteFileOffset(); err != nil {
		return
	}
	if localTinyDeleteFileSize >= repairTask.LeaderTinyDeleteRecordFileSize {
		return
	}
	originLocalTinyDeleteSize = localTinyDeleteFileSize
	isRealSync = true
	dp.lastSyncTinyDeleteTime = time.Now().Unix()
	p := repl.NewPacketToReadTinyDeleteRecord(ctx, dp.partitionID, localTinyDeleteFileSize)
	if conn, err = gConnPool.GetConnect(repairTask.LeaderAddr); err != nil {
		return
	}
	defer gConnPool.PutConnect(conn, true)
	if err = p.WriteToConn(conn, proto.WriteDeadlineTime); err != nil {
		return
	}
	store := dp.extentStore
	start := time.Now().Unix()
	defer func() {
		err = dp.ExtentStore().PlaybackTinyDelete(originLocalTinyDeleteSize)
	}()
	for localTinyDeleteFileSize < repairTask.LeaderTinyDeleteRecordFileSize {
		if localTinyDeleteFileSize >= repairTask.LeaderTinyDeleteRecordFileSize {
			return
		}
		if err = p.ReadFromConn(conn, proto.ReadDeadlineTime); err != nil {
			return
		}
		if p.IsErrPacket() {
			logContent := fmt.Sprintf("action[doStreamFixTinyDeleteRecord] %v.",
				p.LogMessage(p.GetOpMsg(), conn.RemoteAddr().String(), start, fmt.Errorf(string(p.Data[:p.Size]))))
			err = fmt.Errorf(logContent)
			return
		}
		if p.CRC != crc32.ChecksumIEEE(p.Data[:p.Size]) {
			err = fmt.Errorf("crc not match")
			return
		}
		if p.Size%storage.DeleteTinyRecordSize != 0 {
			err = fmt.Errorf("unavali size")
			return
		}
		var index int
		var allTinyDeleteRecordsArr [proto.TinyExtentCount + 1]TinyDeleteRecordArr
		for currTinyExtentID := proto.TinyExtentStartID; currTinyExtentID < proto.TinyExtentStartID+proto.TinyExtentCount; currTinyExtentID++ {
			allTinyDeleteRecordsArr[currTinyExtentID] = make([]TinyDeleteRecord, 0)
		}

		for (index+1)*storage.DeleteTinyRecordSize <= int(p.Size) {
			record := p.Data[index*storage.DeleteTinyRecordSize : (index+1)*storage.DeleteTinyRecordSize]
			extentID, offset, size := storage.UnMarshalTinyExtent(record)
			localTinyDeleteFileSize += storage.DeleteTinyRecordSize
			index++
			if !proto.IsTinyExtent(extentID) {
				continue
			}
			dr := TinyDeleteRecord{
				extentID: extentID,
				offset:   offset,
				size:     size,
			}
			allTinyDeleteRecordsArr[extentID] = append(allTinyDeleteRecordsArr[extentID], dr)
		}
		for currTinyExtentID := proto.TinyExtentStartID; currTinyExtentID < proto.TinyExtentStartID+proto.TinyExtentCount; currTinyExtentID++ {
			currentDeleteRecords := allTinyDeleteRecordsArr[currTinyExtentID]
			for _, dr := range currentDeleteRecords {
				if dr.extentID != uint64(currTinyExtentID) {
					continue
				}
				if !proto.IsTinyExtent(dr.extentID) {
					continue
				}
				store.PersistTinyDeleteRecord(dr.extentID, int64(dr.offset), int64(dr.size))
			}
		}
	}
}

// ChangeRaftMember is a wrapper function of changing the raft member.
func (dp *DataPartition) ChangeRaftMember(changeType raftProto.ConfChangeType, peer raftProto.Peer, context []byte) (resp interface{}, err error) {
	if log.IsWarnEnabled() {
		log.LogWarnf("DP %v: change raft member: type %v, peer %v", dp.partitionID, changeType, peer)
	}
	resp, err = dp.raftPartition.ChangeMember(changeType, peer, context)
	return
}

func (dp *DataPartition) canRemoveSelf() (canRemove bool, err error) {
	var currentPeers []proto.Peer
	var offlinePeerID uint64
	for i := 0; i < 2; i++ {
		if offlinePeerID, currentPeers, err = dp.topologyManager.GetPartitionRaftPeerFromMaster(dp.volumeID, dp.partitionID); err == nil {
			break
		}
	}
	if err != nil {
		log.LogErrorf("action[canRemoveSelf] err(%v)", err)
		return
	}
	canRemove = false
	var existInPeers bool
	for _, peer := range currentPeers {
		if dp.config.NodeID == peer.ID {
			existInPeers = true
		}
	}
	if !existInPeers {
		canRemove = true
		return
	}
	if dp.config.NodeID == offlinePeerID {
		canRemove = true
		return
	}
	return
}

func (dp *DataPartition) SyncReplicaHosts(replicas []string) {
	if len(replicas) == 0 {
		return
	}
	var leader bool // Whether current instance is the leader member.
	if len(replicas) >= 1 {
		leaderAddr := replicas[0]
		leaderAddrParts := strings.Split(leaderAddr, ":")
		if len(leaderAddrParts) == 2 && strings.TrimSpace(leaderAddrParts[0]) == LocalIP {
			leader = true
		}
	}
	dp.replicasLock.Lock()
	dp.isReplLeader = leader
	dp.replicas = replicas
	dp.intervalToUpdateReplicas = time.Now().Unix()
	dp.replicasLock.Unlock()
	log.LogInfof("partition(%v) synchronized replica hosts from master [replicas:(%v), leader: %v]",
		dp.partitionID, strings.Join(replicas, ","), leader)
	if leader {
		dp.proposeRepair()
	}
}

// ResetRaftMember is a wrapper function of changing the raft member.
func (dp *DataPartition) ResetRaftMember(peers []raftProto.Peer, context []byte) (err error) {
	if dp.raftPartition == nil {
		return fmt.Errorf("raft instance not ready")
	}
	err = dp.raftPartition.ResetMember(peers, nil, context)
	return
}

func (dp *DataPartition) EvictExpiredFileDescriptor() {
	dp.extentStore.EvictExpiredCache()
}

func (dp *DataPartition) ForceEvictFileDescriptor(ratio unit.Ratio) {
	dp.extentStore.ForceEvictCache(ratio)
}

func (dp *DataPartition) EvictExpiredExtentDeleteCache(expireTime int64) {
	if expireTime == 0 {
		expireTime = DefaultNormalExtentDeleteExpireTime
	}
	dp.extentStore.EvictExpiredNormalExtentDeleteCache(expireTime)
}

func (dp *DataPartition) getTinyExtentHoleInfo(extent uint64) (result interface{}, err error) {
	holes, extentAvaliSize, err := dp.ExtentStore().TinyExtentHolesAndAvaliSize(extent, 0)
	if err != nil {
		return
	}

	blocks, _ := dp.ExtentStore().GetRealBlockCnt(extent)
	result = &struct {
		Holes           []*proto.TinyExtentHole `json:"holes"`
		ExtentAvaliSize uint64                  `json:"extentAvaliSize"`
		ExtentBlocks    int64                   `json:"blockNum"`
	}{
		Holes:           holes,
		ExtentAvaliSize: extentAvaliSize,
		ExtentBlocks:    blocks,
	}
	return
}

func (dp *DataPartition) getDataPartitionInfo() (dpInfo *DataPartitionViewInfo, err error) {
	var (
		tinyDeleteRecordSize int64
	)
	if tinyDeleteRecordSize, err = dp.ExtentStore().LoadTinyDeleteFileOffset(); err != nil {
		err = fmt.Errorf("load tiny delete file offset fail: %v", err)
		return
	}
	var raftStatus *raft.Status
	if dp.raftPartition != nil {
		raftStatus = dp.raftPartition.Status()
	}
	dpInfo = &DataPartitionViewInfo{
		VolName:              dp.volumeID,
		ID:                   dp.partitionID,
		Size:                 dp.Size(),
		Used:                 dp.Used(),
		Status:               dp.Status(),
		Path:                 dp.Path(),
		Replicas:             dp.getReplicaClone(),
		TinyDeleteRecordSize: tinyDeleteRecordSize,
		RaftStatus:           raftStatus,
		Peers:                dp.config.Peers,
		Learners:             dp.config.Learners,
		IsFinishLoad:         dp.ExtentStore().IsFinishLoad(),
		IsRecover:            dp.DataPartitionCreateType == proto.DecommissionedCreateDataPartition,
		BaseExtentID:         dp.ExtentStore().GetBaseExtentID(),
		RiskFixerStatus: func() *riskdata.FixerStatus {
			if dp.dataFixer != nil {
				return dp.dataFixer.Status()
			}
			return nil
		}(),
	}
	return
}

func (dp *DataPartition) setFaultOccurredCheckLevel(checkCorruptLevel FaultOccurredCheckLevel) {
	dp.serverFaultCheckLevel = checkCorruptLevel
}

func (dp *DataPartition) ChangeCreateType(createType int) (err error) {
	if dp.DataPartitionCreateType != createType {
		dp.DataPartitionCreateType = createType
		err = dp.persistMetaDataOnly()
		return
	}
	return
}

func (dp *DataPartition) scanIssueFragments(latestFlushTimeUnix int64) (fragments []*riskdata.Fragment, err error) {
	if latestFlushTimeUnix == 0 {
		return
	}
	// 触发所有Extent必要元信息的加载或等待异步加载结束以在接下来的处理可以获得存储引擎中所有Extent的准确元信息。
	dp.extentStore.Load()

	var latestFlushTime = time.Unix(latestFlushTimeUnix, 0)
	var safetyTime = latestFlushTime.Add(-time.Second)
	// 对存储引擎中的所有数据块进行过滤，将有数据(Size > 0)且修改时间晚于最近一次Flush的Extent过滤出来进行接下来的检查和修复。
	dp.extentStore.WalkExtentsInfo(func(info *storage.ExtentInfoBlock) {
		if log.IsDebugEnabled() {
			log.LogDebugf("scanIssueFragments Partition(%v)_Extent(%v)_ModifyTime(%v), safetyTime(%v)", dp.partitionID, info[storage.FileID], info[storage.ModifyTime], safetyTime)
		}
		if info[storage.Size] > 0 && time.Unix(int64(info[storage.ModifyTime]), 0).After(safetyTime) {
			var (
				extentID   = info[storage.FileID]
				extentSize = info[storage.Size]

				fragOffset uint64 = 0
				fragSize          = extentSize
			)
			if proto.IsTinyExtent(extentID) {
				var err error
				if extentSize, err = dp.extentStore.TinyExtentGetFinfoSize(extentID); err != nil {
					if log.IsWarnEnabled() {
						log.LogWarnf("Partition(%v) can not get file info size for tiny Extent(%v): %v", dp.partitionID, extentID, err)
						return
					}
				}
				if extentSize > 128*unit.MB {
					fragOffset = extentSize - 128*unit.MB
				}
				// 按512个字节对齐
				if fragOffset%512 != 0 {
					fragOffset = (fragOffset / 512) * 512
				}
				fragSize = extentSize - fragOffset
			}
			// 切成最大16MB的段
			for subFragOffset := fragOffset; subFragOffset < extentSize; {
				subFragSize := uint64(math.Min(float64(16*unit.MB), float64((fragOffset+fragSize)-subFragOffset)))
				fragments = append(fragments, &riskdata.Fragment{
					ExtentID: extentID,
					Offset:   subFragOffset,
					Size:     subFragSize,
				})
				subFragOffset += subFragSize
			}

		}
	})
	return
}

func convertCheckCorruptLevel(l uint64) (FaultOccurredCheckLevel, error) {
	switch l {
	case 0:
		return CheckNothing, nil
	case 1:
		return CheckQuorumCommitID, nil
	case 2:
		return CheckAllCommitID, nil
	default:
		return CheckNothing, fmt.Errorf("invalid param")
	}
}

func (dp *DataPartition) limit(ctx context.Context, op int, size uint32, bandType string) (err error) {
	if dp == nil {
		return ErrPartitionNil
	}
	prBuilder := multirate.NewPropertiesBuilder().SetOp(strconv.Itoa(op)).SetVol(dp.volumeID).SetDisk(dp.disk.Path)
	stBuilder := multirate.NewStatBuilder().SetCount(1)

	switch op {
	case int(proto.OpWrite), int(proto.OpSyncWrite), int(proto.OpRandomWrite), int(proto.OpSyncRandomWrite), proto.OpExtentRepairWrite_, proto.OpExtentRepairWriteToApplyTempFile_:
		prBuilder.SetBandType(bandType)
		stBuilder.SetInBytes(int(size))
	case int(proto.OpStreamRead), int(proto.OpRead), int(proto.OpStreamFollowerRead), int(proto.OpTinyExtentRepairRead), int(proto.OpTinyExtentAvaliRead),
		int(proto.OpExtentRepairRead), proto.OpExtentRepairReadToRollback_, proto.OpExtentRepairReadToComputeCrc_, proto.OpExtentReadToGetCrc_:
		prBuilder.SetBandType(bandType).Properties()
		stBuilder.SetOutBytes(int(size)).Stat()
	default:
	}
	err = multirate.WaitNUseDefaultTimeout(ctx, prBuilder.Properties(), stBuilder.Stat())
	if err != nil {
		err = errors.Trace(err, proto.RateLimit)
	}
	return
}

func (dp *DataPartition) backendRefreshCacheView() {
	if dp.topologyManager == nil {
		return
	}
	dp.topologyManager.FetchDataPartitionView(dp.volumeID, dp.partitionID)
}

func (dp *DataPartition) getCacheView() (dataPartition *topology.DataPartition, err error) {
	if dp.topologyManager == nil {
		return nil, fmt.Errorf("topo manager is nil")
	}
	return dp.topologyManager.GetPartition(dp.volumeID, dp.partitionID)
}
