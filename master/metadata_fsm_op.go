// Copyright 2018 The Chubao Authors.
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

package master

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	bsProto "github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/util/errors"
	"github.com/chubaofs/chubaofs/util/log"
	"github.com/tiglabs/raft/proto"
)

/* We defines several "values" such as clusterValue, metaPartitionValue, dataPartitionValue, volValue, dataNodeValue,
   nodeSetValue, and metaNodeValue here. Those are the value objects that will be marshaled as a byte array to
   transferred over the network. */

type clusterValue struct {
	Name                              string
	Threshold                         float32
	DisableAutoAllocate               bool
	DataNodeDeleteLimitRate           uint64
	DataNodeRepairTaskCount           uint64
	DataNodeReqZoneRateLimitMap       map[string]uint64
	DataNodeReqZoneOpRateLimitMap     map[string]map[uint8]uint64
	DataNodeReqZoneVolOpRateLimitMap  map[string]map[string]map[uint8]uint64
	DataNodeReqVolPartRateLimitMap    map[string]uint64
	DataNodeReqVolOpPartRateLimitMap  map[string]map[uint8]uint64
	MetaNodeReqRateLimit              uint64
	MetaNodeReqOpRateLimitMap         map[uint8]uint64
	MetaNodeDeleteBatchCount          uint64
	MetaNodeDeleteWorkerSleepMs       uint64
	ClientReadVolRateLimitMap         map[string]uint64
	ClientWriteVolRateLimitMap        map[string]uint64
	ClientVolOpRateLimitMap           map[string]map[uint8]int64
	PoolSizeOfDataPartitionsInRecover int32
	PoolSizeOfMetaPartitionsInRecover int32
	ExtentMergeIno                    map[string][]uint64
	ExtentMergeSleepMs                uint64
	FixTinyDeleteRecordLimit          uint64
}

func newClusterValue(c *Cluster) (cv *clusterValue) {
	cv = &clusterValue{
		Name:                              c.Name,
		Threshold:                         c.cfg.MetaNodeThreshold,
		DataNodeDeleteLimitRate:           c.cfg.DataNodeDeleteLimitRate,
		DataNodeRepairTaskCount:           c.cfg.DataNodeRepairTaskCount,
		DataNodeReqZoneRateLimitMap:       c.cfg.DataNodeReqZoneRateLimitMap,
		DataNodeReqZoneOpRateLimitMap:     c.cfg.DataNodeReqZoneOpRateLimitMap,
		DataNodeReqZoneVolOpRateLimitMap:  c.cfg.DataNodeReqZoneVolOpRateLimitMap,
		DataNodeReqVolPartRateLimitMap:    c.cfg.DataNodeReqVolPartRateLimitMap,
		DataNodeReqVolOpPartRateLimitMap:  c.cfg.DataNodeReqVolOpPartRateLimitMap,
		MetaNodeReqRateLimit:              c.cfg.MetaNodeReqRateLimit,
		MetaNodeReqOpRateLimitMap:         c.cfg.MetaNodeReqOpRateLimitMap,
		MetaNodeDeleteBatchCount:          c.cfg.MetaNodeDeleteBatchCount,
		MetaNodeDeleteWorkerSleepMs:       c.cfg.MetaNodeDeleteWorkerSleepMs,
		ClientReadVolRateLimitMap:         c.cfg.ClientReadVolRateLimitMap,
		ClientWriteVolRateLimitMap:        c.cfg.ClientWriteVolRateLimitMap,
		ClientVolOpRateLimitMap:           c.cfg.ClientVolOpRateLimitMap,
		DisableAutoAllocate:               c.DisableAutoAllocate,
		PoolSizeOfDataPartitionsInRecover: c.cfg.DataPartitionsRecoverPoolSize,
		PoolSizeOfMetaPartitionsInRecover: c.cfg.MetaPartitionsRecoverPoolSize,
		ExtentMergeIno:                    c.cfg.ExtentMergeIno,
		ExtentMergeSleepMs:                c.cfg.ExtentMergeSleepMs,
		FixTinyDeleteRecordLimit:          c.dnFixTinyDeleteRecordLimit,
	}
	return cv
}

type metaPartitionValue struct {
	PartitionID   uint64
	Start         uint64
	End           uint64
	VolID         uint64
	ReplicaNum    uint8
	LearnerNum    uint8
	Status        int8
	VolName       string
	Hosts         string
	OfflinePeerID uint64
	Peers         []bsProto.Peer
	Learners      []bsProto.Learner
	PanicHosts    []string
	IsRecover     bool
}

func newMetaPartitionValue(mp *MetaPartition) (mpv *metaPartitionValue) {
	mpv = &metaPartitionValue{
		PartitionID:   mp.PartitionID,
		Start:         mp.Start,
		End:           mp.End,
		VolID:         mp.volID,
		ReplicaNum:    mp.ReplicaNum,
		LearnerNum:    mp.LearnerNum,
		Status:        mp.Status,
		VolName:       mp.volName,
		Hosts:         mp.hostsToString(),
		Peers:         mp.Peers,
		Learners:      mp.Learners,
		OfflinePeerID: mp.OfflinePeerID,
		IsRecover:     mp.IsRecover,
		PanicHosts:    mp.PanicHosts,
	}
	return
}

type dataPartitionValue struct {
	PartitionID   uint64
	ReplicaNum    uint8
	Hosts         string
	Peers         []bsProto.Peer
	Learners      []bsProto.Learner
	Status        int8
	VolID         uint64
	VolName       string
	OfflinePeerID uint64
	Replicas      []*replicaValue
	IsRecover     bool
	PanicHosts    []string
}

type replicaValue struct {
	Addr     string
	DiskPath string
}

func newDataPartitionValue(dp *DataPartition) (dpv *dataPartitionValue) {
	dpv = &dataPartitionValue{
		PartitionID:   dp.PartitionID,
		ReplicaNum:    dp.ReplicaNum,
		Hosts:         dp.hostsToString(),
		Peers:         dp.Peers,
		Learners:      dp.Learners,
		Status:        dp.Status,
		VolID:         dp.VolID,
		VolName:       dp.VolName,
		OfflinePeerID: dp.OfflinePeerID,
		PanicHosts:    dp.PanicHosts,
		Replicas:      make([]*replicaValue, 0),
		IsRecover:     dp.isRecover,
	}
	for _, replica := range dp.Replicas {
		rv := &replicaValue{Addr: replica.Addr, DiskPath: replica.DiskPath}
		dpv.Replicas = append(dpv.Replicas, rv)
	}
	return
}

type volValue struct {
	ID                  uint64
	Name                string
	ReplicaNum          uint8
	DpReplicaNum        uint8
	MpLearnerNum        uint8
	DpLearnerNum        uint8
	Status              uint8
	DataPartitionSize   uint64
	Capacity            uint64
	DpWriteableThreshold float64
	Owner               string
	FollowerRead        bool
	ForceROW            bool
	CrossRegionHAType   bsProto.CrossRegionHAType
	Authenticate        bool
	EnableToken         bool
	CrossZone           bool
	AutoRepair          bool
	VolWriteMutexEnable bool
	ZoneName            string
	OSSAccessKey        string
	OSSSecretKey        string
	CreateTime          int64
	Description         string
	DpSelectorName      string
	DpSelectorParm      string
	OSSBucketPolicy     bsProto.BucketAccessPolicy
	DPConvertMode       bsProto.ConvertMode
	MPConvertMode       bsProto.ConvertMode
	ExtentCacheExpireSec	int64
	MinWritableMPNum        int
	MinWritableDPNum        int
}

func (v *volValue) Bytes() (raw []byte, err error) {
	raw, err = json.Marshal(v)
	return
}

func newVolValue(vol *Vol) (vv *volValue) {
	vv = &volValue{
		ID:                  vol.ID,
		Name:                vol.Name,
		ReplicaNum:          vol.mpReplicaNum,
		DpReplicaNum:        vol.dpReplicaNum,
		Status:              vol.Status,
		DataPartitionSize:   vol.dataPartitionSize,
		Capacity:            vol.Capacity,
		Owner:               vol.Owner,
		FollowerRead:        vol.FollowerRead,
		ForceROW:            vol.ForceROW,
		CrossRegionHAType:   vol.CrossRegionHAType,
		Authenticate:        vol.authenticate,
		AutoRepair:          vol.autoRepair,
		VolWriteMutexEnable: vol.volWriteMutexEnable,
		ZoneName:            vol.zoneName,
		CrossZone:           vol.crossZone,
		EnableToken:         vol.enableToken,
		OSSAccessKey:        vol.OSSAccessKey,
		OSSSecretKey:        vol.OSSSecretKey,
		CreateTime:          vol.createTime,
		Description:         vol.description,
		DpSelectorName:      vol.dpSelectorName,
		DpSelectorParm:      vol.dpSelectorParm,
		OSSBucketPolicy:     vol.OSSBucketPolicy,
		DpWriteableThreshold: vol.dpWriteableThreshold,
		DpLearnerNum:        vol.dpLearnerNum,
		MpLearnerNum:        vol.mpLearnerNum,
		DPConvertMode:       vol.DPConvertMode,
		MPConvertMode:       vol.MPConvertMode,
		ExtentCacheExpireSec: vol.ExtentCacheExpireSec,
		MinWritableMPNum:     vol.MinWritableMPNum,
		MinWritableDPNum:     vol.MinWritableDPNum,
	}
	return
}

func newVolValueFromBytes(raw []byte) (*volValue, error) {
	vv := &volValue{}
	if err := json.Unmarshal(raw, vv); err != nil {
		return nil, err
	}
	return vv, nil
}

type dataNodeValue struct {
	ID        uint64
	NodeSetID uint64
	Addr      string
	ZoneName  string
}

func newDataNodeValue(dataNode *DataNode) *dataNodeValue {
	return &dataNodeValue{
		ID:        dataNode.ID,
		NodeSetID: dataNode.NodeSetID,
		Addr:      dataNode.Addr,
		ZoneName:  dataNode.ZoneName,
	}
}

type metaNodeValue struct {
	ID        uint64
	NodeSetID uint64
	Addr      string
	ZoneName  string
}

func newMetaNodeValue(metaNode *MetaNode) *metaNodeValue {
	return &metaNodeValue{
		ID:        metaNode.ID,
		NodeSetID: metaNode.NodeSetID,
		Addr:      metaNode.Addr,
		ZoneName:  metaNode.ZoneName,
	}
}

type nodeSetValue struct {
	ID       uint64
	Capacity int
	ZoneName string
}

func newNodeSetValue(nset *nodeSet) (nsv *nodeSetValue) {
	nsv = &nodeSetValue{
		ID:       nset.ID,
		Capacity: nset.Capacity,
		ZoneName: nset.zoneName,
	}
	return
}

type regionValue struct {
	Name       string
	Zones      []string
	RegionType bsProto.RegionType
}

func newRegionValue(region *Region) (rv *regionValue) {
	rv = &regionValue{
		Name:       region.Name,
		RegionType: region.RegionType,
		Zones:      region.getZones(),
	}
	return
}

// RaftCmd defines the Raft commands.
type RaftCmd struct {
	Op uint32 `json:"op"`
	K  string `json:"k"`
	V  []byte `json:"v"`
}

// Marshal converts the RaftCmd to a byte array.
func (m *RaftCmd) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal converts the byte array to a RaftCmd.
func (m *RaftCmd) Unmarshal(data []byte) (err error) {
	return json.Unmarshal(data, m)
}

func (m *RaftCmd) setOpType() {
	keyArr := strings.Split(m.K, keySeparator)
	if len(keyArr) < 2 {
		log.LogWarnf("action[setOpType] invalid length[%v]", keyArr)
		return
	}
	switch keyArr[1] {
	case metaNodeAcronym:
		m.Op = opSyncAddMetaNode
	case dataNodeAcronym:
		m.Op = opSyncAddDataNode
	case dataPartitionAcronym:
		m.Op = opSyncAddDataPartition
	case metaPartitionAcronym:
		m.Op = opSyncAddMetaPartition
	case volAcronym:
		m.Op = opSyncAddVol
	case clusterAcronym:
		m.Op = opSyncPutCluster
	case nodeSetAcronym:
		m.Op = opSyncAddNodeSet
	case maxDataPartitionIDKey:
		m.Op = opSyncAllocDataPartitionID
	case maxMetaPartitionIDKey:
		m.Op = opSyncAllocMetaPartitionID
	case maxCommonIDKey:
		m.Op = opSyncAllocCommonID
	case userAcronym:
		m.Op = opSyncAddUserInfo
	case akAcronym:
		m.Op = opSyncAddAKUser
	case volUserAcronym:
		m.Op = opSyncAddVolUser
	case tokenAcronym:
		m.Op = OpSyncAddToken
	case regionAcronym:
		m.Op = OpSyncAddRegion
	default:
		log.LogWarnf("action[setOpType] unknown opCode[%v]", keyArr[1])
	}
}

func (c *Cluster) syncDeleteToken(token *bsProto.Token) (err error) {
	return c.syncPutTokenInfo(OpSyncDelToken, token)
}

func (c *Cluster) syncAddToken(token *bsProto.Token) (err error) {
	return c.syncPutTokenInfo(OpSyncAddToken, token)
}

func (c *Cluster) syncUpdateToken(token *bsProto.Token) (err error) {
	return c.syncPutTokenInfo(OpSyncUpdateToken, token)
}

func (c *Cluster) syncPutTokenInfo(opType uint32, token *bsProto.Token) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = TokenPrefix + token.VolName + keySeparator + token.Value
	tv := newTokenValue(token)
	metadata.V, err = json.Marshal(tv)
	if err != nil {
		return
	}
	return c.submit(metadata)
}

//key=#c#name
func (c *Cluster) syncPutCluster() (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opSyncPutCluster
	metadata.K = clusterPrefix + c.Name
	cv := newClusterValue(c)
	metadata.V, err = json.Marshal(cv)
	if err != nil {
		return
	}
	return c.submit(metadata)
}

// key=#s#id
func (c *Cluster) syncAddNodeSet(nset *nodeSet) (err error) {
	return c.putNodeSetInfo(opSyncAddNodeSet, nset)
}

func (c *Cluster) syncUpdateNodeSet(nset *nodeSet) (err error) {
	return c.putNodeSetInfo(opSyncUpdateNodeSet, nset)
}

func (c *Cluster) putNodeSetInfo(opType uint32, nset *nodeSet) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = nodeSetPrefix + strconv.FormatUint(nset.ID, 10)
	nsv := newNodeSetValue(nset)
	metadata.V, err = json.Marshal(nsv)
	if err != nil {
		return
	}
	return c.submit(metadata)
}

// key=#dp#volID#partitionID,value=json.Marshal(dataPartitionValue)
func (c *Cluster) syncAddDataPartition(dp *DataPartition) (err error) {
	return c.putDataPartitionInfo(opSyncAddDataPartition, dp)
}

func (c *Cluster) syncUpdateDataPartition(dp *DataPartition) (err error) {
	return c.putDataPartitionInfo(opSyncUpdateDataPartition, dp)
}

func (c *Cluster) syncDeleteDataPartition(dp *DataPartition) (err error) {
	return c.putDataPartitionInfo(opSyncDeleteDataPartition, dp)
}

func (c *Cluster) putDataPartitionInfo(opType uint32, dp *DataPartition) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = dataPartitionPrefix + strconv.FormatUint(dp.VolID, 10) + keySeparator + strconv.FormatUint(dp.PartitionID, 10)
	dpv := newDataPartitionValue(dp)
	metadata.V, err = json.Marshal(dpv)
	if err != nil {
		return
	}
	return c.submit(metadata)
}

func (c *Cluster) submit(metadata *RaftCmd) (err error) {
	cmd, err := metadata.Marshal()
	if err != nil {
		return errors.New(err.Error())
	}
	if _, err = c.partition.Submit(cmd); err != nil {
		msg := fmt.Sprintf("action[metadata_submit] err:%v", err.Error())
		return errors.New(msg)
	}
	return
}

//key=#vol#volID,value=json.Marshal(vv)
func (c *Cluster) syncAddVol(vol *Vol) (err error) {
	return c.syncPutVolInfo(opSyncAddVol, vol)
}

func (c *Cluster) syncUpdateVol(vol *Vol) (err error) {
	return c.syncPutVolInfo(opSyncUpdateVol, vol)
}

func (c *Cluster) syncDeleteVol(vol *Vol) (err error) {
	return c.syncPutVolInfo(opSyncDeleteVol, vol)
}

func (c *Cluster) syncPutVolInfo(opType uint32, vol *Vol) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = volPrefix + strconv.FormatUint(vol.ID, 10)
	vv := newVolValue(vol)
	if metadata.V, err = json.Marshal(vv); err != nil {
		return errors.New(err.Error())
	}
	return c.submit(metadata)
}

// key=#mp#volID#metaPartitionID,value=json.Marshal(metaPartitionValue)
func (c *Cluster) syncAddMetaPartition(mp *MetaPartition) (err error) {
	return c.putMetaPartitionInfo(opSyncAddMetaPartition, mp)
}

func (c *Cluster) syncUpdateMetaPartition(mp *MetaPartition) (err error) {
	return c.putMetaPartitionInfo(opSyncUpdateMetaPartition, mp)
}

func (c *Cluster) syncDeleteMetaPartition(mp *MetaPartition) (err error) {
	return c.putMetaPartitionInfo(opSyncDeleteMetaPartition, mp)
}

func (c *Cluster) putMetaPartitionInfo(opType uint32, mp *MetaPartition) (err error) {
	metadata, err := c.buildMetaPartitionRaftCmd(opType, mp)
	if err != nil {
		return
	}
	return c.submit(metadata)
}

func (c *Cluster) buildMetaPartitionRaftCmd(opType uint32, mp *MetaPartition) (metadata *RaftCmd, err error) {
	metadata = new(RaftCmd)
	metadata.Op = opType
	partitionID := strconv.FormatUint(mp.PartitionID, 10)
	metadata.K = metaPartitionPrefix + strconv.FormatUint(mp.volID, 10) + keySeparator + partitionID
	mpv := newMetaPartitionValue(mp)
	if metadata.V, err = json.Marshal(mpv); err != nil {
		return metadata, errors.New(err.Error())
	}
	return
}

func (c *Cluster) syncBatchCommitCmd(cmdMap map[string]*RaftCmd) (err error) {
	value, err := json.Marshal(cmdMap)
	if err != nil {
		return
	}
	cmd := &RaftCmd{
		Op: opSyncBatchPut,
		K:  "batch_put",
		V:  value,
	}
	return c.submit(cmd)
}

// key=#mn#id#addr,value = nil
func (c *Cluster) syncAddMetaNode(metaNode *MetaNode) (err error) {
	return c.syncPutMetaNode(opSyncAddMetaNode, metaNode)
}

func (c *Cluster) syncDeleteMetaNode(metaNode *MetaNode) (err error) {
	return c.syncPutMetaNode(opSyncDeleteMetaNode, metaNode)
}

func (c *Cluster) syncUpdateMetaNode(metaNode *MetaNode) (err error) {
	return c.syncPutMetaNode(opSyncUpdateMetaNode, metaNode)
}

func (c *Cluster) syncPutMetaNode(opType uint32, metaNode *MetaNode) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = metaNodePrefix + strconv.FormatUint(metaNode.ID, 10) + keySeparator + metaNode.Addr
	mnv := newMetaNodeValue(metaNode)
	metadata.V, err = json.Marshal(mnv)
	if err != nil {
		return errors.New(err.Error())
	}
	return c.submit(metadata)
}

// key=#dn#id#Addr,value = json.Marshal(dnv)
func (c *Cluster) syncAddDataNode(dataNode *DataNode) (err error) {
	return c.syncPutDataNodeInfo(opSyncAddDataNode, dataNode)
}

func (c *Cluster) syncDeleteDataNode(dataNode *DataNode) (err error) {
	return c.syncPutDataNodeInfo(opSyncDeleteDataNode, dataNode)
}

func (c *Cluster) syncUpdateDataNode(dataNode *DataNode) (err error) {
	return c.syncPutDataNodeInfo(opSyncUpdateDataNode, dataNode)
}

func (c *Cluster) syncPutDataNodeInfo(opType uint32, dataNode *DataNode) (err error) {
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = dataNodePrefix + strconv.FormatUint(dataNode.ID, 10) + keySeparator + dataNode.Addr
	dnv := newDataNodeValue(dataNode)
	metadata.V, err = json.Marshal(dnv)
	if err != nil {
		return errors.New(err.Error())
	}
	return c.submit(metadata)
}

func (c *Cluster) addRaftNode(nodeID uint64, addr string) (err error) {
	peer := proto.Peer{ID: nodeID}
	_, err = c.partition.ChangeMember(proto.ConfAddNode, peer, []byte(addr))
	if err != nil {
		return errors.New("action[addRaftNode] error: " + err.Error())
	}
	return nil
}

func (c *Cluster) removeRaftNode(nodeID uint64, addr string) (err error) {
	peer := proto.Peer{ID: nodeID}
	_, err = c.partition.ChangeMember(proto.ConfRemoveNode, peer, []byte(addr))
	if err != nil {
		return errors.New("action[removeRaftNode] error: " + err.Error())
	}
	return nil
}

func (c *Cluster) updateMetaNodeDeleteBatchCount(val uint64) {
	atomic.StoreUint64(&c.cfg.MetaNodeDeleteBatchCount, val)
}

func (c *Cluster) updateMetaNodeDeleteWorkerSleepMs(val uint64) {
	atomic.StoreUint64(&c.cfg.MetaNodeDeleteWorkerSleepMs, val)
}
func (c *Cluster) updateRecoverPoolSize(dpPoolSize, mpPoolSize int32) {
	if dpPoolSize == 0 {
		dpPoolSize = defaultRecoverPoolSize
	}
	if mpPoolSize == 0 {
		mpPoolSize = defaultRecoverPoolSize
	}
	atomic.StoreInt32(&c.cfg.DataPartitionsRecoverPoolSize, dpPoolSize)
	atomic.StoreInt32(&c.cfg.MetaPartitionsRecoverPoolSize, mpPoolSize)

}
func (c *Cluster) updateDataNodeDeleteLimitRate(val uint64) {
	atomic.StoreUint64(&c.cfg.DataNodeDeleteLimitRate, val)
}

//key=#region#regionName,value=json.Marshal(rv)
func (c *Cluster) syncAddRegion(region *Region) (err error) {
	return c.syncPutRegionInfo(OpSyncAddRegion, region)
}

func (c *Cluster) syncUpdateRegion(region *Region) (err error) {
	return c.syncPutRegionInfo(OpSyncUpdateRegion, region)
}

func (c *Cluster) syncDelRegion(region *Region) (err error) {
	return c.syncPutRegionInfo(OpSyncDelRegion, region)
}

func (c *Cluster) syncPutRegionInfo(opType uint32, region *Region) (err error) {
	if region == nil {
		return fmt.Errorf("action[syncPutRegionInfo] region is nil")
	}
	metadata := new(RaftCmd)
	metadata.Op = opType
	metadata.K = regionPrefix + region.Name
	rv := newRegionValue(region)
	if metadata.V, err = json.Marshal(rv); err != nil {
		return errors.New(err.Error())
	}
	return c.submit(metadata)
}

func (c *Cluster) loadClusterValue() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(clusterPrefix))
	if err != nil {
		err = fmt.Errorf("action[loadClusterValue],err:%v", err.Error())
		return err
	}
	for _, value := range result {
		cv := &clusterValue{}
		if err = json.Unmarshal(value, cv); err != nil {
			log.LogErrorf("action[loadClusterValue], unmarshal err:%v", err.Error())
			return err
		}
		c.cfg.MetaNodeThreshold = cv.Threshold
		c.DisableAutoAllocate = cv.DisableAutoAllocate
		if cv.FixTinyDeleteRecordLimit <= 0 {
			cv.FixTinyDeleteRecordLimit = 1
		}
		c.dnFixTinyDeleteRecordLimit = cv.FixTinyDeleteRecordLimit
		c.updateMetaNodeDeleteBatchCount(cv.MetaNodeDeleteBatchCount)
		c.updateMetaNodeDeleteWorkerSleepMs(cv.MetaNodeDeleteWorkerSleepMs)
		atomic.StoreUint64(&c.cfg.MetaNodeReqRateLimit, cv.MetaNodeReqRateLimit)
		c.cfg.MetaNodeReqOpRateLimitMap = cv.MetaNodeReqOpRateLimitMap
		if c.cfg.MetaNodeReqOpRateLimitMap == nil {
			c.cfg.MetaNodeReqOpRateLimitMap = make(map[uint8]uint64)
		}
		c.updateDataNodeDeleteLimitRate(cv.DataNodeDeleteLimitRate)
		atomic.StoreUint64(&c.cfg.DataNodeRepairTaskCount, cv.DataNodeRepairTaskCount)
		c.cfg.DataNodeReqZoneRateLimitMap = cv.DataNodeReqZoneRateLimitMap
		if c.cfg.DataNodeReqZoneRateLimitMap == nil {
			c.cfg.DataNodeReqZoneRateLimitMap = make(map[string]uint64)
		}
		c.cfg.DataNodeReqZoneOpRateLimitMap = cv.DataNodeReqZoneOpRateLimitMap
		if c.cfg.DataNodeReqZoneOpRateLimitMap == nil {
			c.cfg.DataNodeReqZoneOpRateLimitMap = make(map[string]map[uint8]uint64)
		}
		c.cfg.DataNodeReqZoneVolOpRateLimitMap = cv.DataNodeReqZoneVolOpRateLimitMap
		if c.cfg.DataNodeReqZoneVolOpRateLimitMap == nil {
			c.cfg.DataNodeReqZoneVolOpRateLimitMap = make(map[string]map[string]map[uint8]uint64)
		}
		c.cfg.DataNodeReqVolPartRateLimitMap = cv.DataNodeReqVolPartRateLimitMap
		if c.cfg.DataNodeReqVolPartRateLimitMap == nil {
			c.cfg.DataNodeReqVolPartRateLimitMap = make(map[string]uint64)
		}
		c.cfg.DataNodeReqVolOpPartRateLimitMap = cv.DataNodeReqVolOpPartRateLimitMap
		if c.cfg.DataNodeReqVolOpPartRateLimitMap == nil {
			c.cfg.DataNodeReqVolOpPartRateLimitMap = make(map[string]map[uint8]uint64)
		}
		c.cfg.ClientReadVolRateLimitMap = cv.ClientReadVolRateLimitMap
		if c.cfg.ClientReadVolRateLimitMap == nil {
			c.cfg.ClientReadVolRateLimitMap = make(map[string]uint64)
		}
		c.cfg.ClientWriteVolRateLimitMap = cv.ClientWriteVolRateLimitMap
		if c.cfg.ClientWriteVolRateLimitMap == nil {
			c.cfg.ClientWriteVolRateLimitMap = make(map[string]uint64)
		}
		c.cfg.ClientVolOpRateLimitMap = cv.ClientVolOpRateLimitMap
		if c.cfg.ClientVolOpRateLimitMap == nil {
			c.cfg.ClientVolOpRateLimitMap = make(map[string]map[uint8]int64)
		}
		c.updateRecoverPoolSize(cv.PoolSizeOfDataPartitionsInRecover, cv.PoolSizeOfMetaPartitionsInRecover)
		c.cfg.ExtentMergeIno = cv.ExtentMergeIno
		if c.cfg.ExtentMergeIno == nil {
			c.cfg.ExtentMergeIno = make(map[string][]uint64)
		}
		atomic.StoreUint64(&c.cfg.ExtentMergeSleepMs, cv.ExtentMergeSleepMs)
		log.LogInfof("action[loadClusterValue], cv[%v]", cv)
	}
	return
}

func (c *Cluster) loadNodeSets() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(nodeSetPrefix))
	if err != nil {
		err = fmt.Errorf("action[loadNodeSets],err:%v", err.Error())
		return err
	}
	for _, value := range result {
		nsv := &nodeSetValue{}
		if err = json.Unmarshal(value, nsv); err != nil {
			log.LogErrorf("action[loadNodeSets], unmarshal err:%v", err.Error())
			return err
		}
		if nsv.ZoneName == "" {
			nsv.ZoneName = DefaultZoneName
		}
		ns := newNodeSet(nsv.ID, c.cfg.nodeSetCapacity, nsv.ZoneName)
		zone, err := c.t.getZone(nsv.ZoneName)
		if err != nil {
			log.LogErrorf("action[loadNodeSets], getZone err:%v", err)
			zone = newZone(nsv.ZoneName)
			c.t.putZoneIfAbsent(zone)
		}
		zone.putNodeSet(ns)
		log.LogInfof("action[loadNodeSets], nsId[%v],zone[%v]", ns.ID, zone.name)
	}
	return
}

func (c *Cluster) loadDataNodes() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(dataNodePrefix))
	if err != nil {
		err = fmt.Errorf("action[loadDataNodes],err:%v", err.Error())
		return err
	}

	for _, value := range result {
		dnv := &dataNodeValue{}
		if err = json.Unmarshal(value, dnv); err != nil {
			err = fmt.Errorf("action[loadDataNodes],value:%v,unmarshal err:%v", string(value), err)
			return
		}
		if dnv.ZoneName == "" {
			dnv.ZoneName = DefaultZoneName
		}
		dataNode := newDataNode(dnv.Addr, dnv.ZoneName, c.Name)
		dataNode.ID = dnv.ID
		dataNode.NodeSetID = dnv.NodeSetID
		c.dataNodes.Store(dataNode.Addr, dataNode)
		log.LogInfof("action[loadDataNodes],dataNode[%v],id[%v],zone[%v],ns[%v]", dataNode.Addr, dnv.ID, dnv.ZoneName, dnv.NodeSetID)
	}
	return
}

func (c *Cluster) loadMetaNodes() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(metaNodePrefix))
	if err != nil {
		err = fmt.Errorf("action[loadMetaNodes],err:%v", err.Error())
		return err
	}
	for _, value := range result {
		mnv := &metaNodeValue{}
		if err = json.Unmarshal(value, mnv); err != nil {
			err = fmt.Errorf("action[loadMetaNodes],unmarshal err:%v", err.Error())
			return err
		}
		if mnv.ZoneName == "" {
			mnv.ZoneName = DefaultZoneName
		}
		metaNode := newMetaNode(mnv.Addr, mnv.ZoneName, c.Name)
		metaNode.ID = mnv.ID
		metaNode.NodeSetID = mnv.NodeSetID
		c.metaNodes.Store(metaNode.Addr, metaNode)
		log.LogInfof("action[loadMetaNodes],metaNode[%v],id[%v],zone[%v],ns[%v]", metaNode.Addr, mnv.ID, mnv.ZoneName, mnv.NodeSetID)
	}
	return
}

func (c *Cluster) loadVols() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(volPrefix))
	if err != nil {
		err = fmt.Errorf("action[loadVols],err:%v", err.Error())
		return err
	}
	for _, value := range result {
		var vv *volValue
		if vv, err = newVolValueFromBytes(value); err != nil {
			err = fmt.Errorf("action[loadVols],value:%v,unmarshal err:%v", string(value), err)
			return err
		}
		if !vv.CrossZone && vv.ZoneName == "" {
			vv.ZoneName = DefaultZoneName
		}
		if vv.ExtentCacheExpireSec == 0 {
			vv.ExtentCacheExpireSec = defaultExtentCacheExpireSec
		}
		// TODO volume mutex
		vol := newVolFromVolValue(vv)
		vol.Status = vv.Status
		c.putVol(vol)
		log.LogInfof("action[loadVols],vol[%v],id[%v],status[%v]", vol.Name, vv.ID, vv.Status)
	}
	return
}

func (c *Cluster) loadMetaPartitions() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(metaPartitionPrefix))
	if err != nil {
		err = fmt.Errorf("action[loadMetaPartitions],err:%v", err.Error())
		return err
	}

	for _, value := range result {
		mpv := &metaPartitionValue{}
		if err = json.Unmarshal(value, mpv); err != nil {
			err = fmt.Errorf("action[loadMetaPartitions],value:%v,unmarshal err:%v", string(value), err)
			return err
		}
		vol, err1 := c.getVol(mpv.VolName)
		if err1 != nil {
			log.LogErrorf("action[loadMetaPartitions] err:%v", err1.Error())
			continue
		}
		if vol.ID != mpv.VolID {
			Warn(c.Name, fmt.Sprintf("action[loadMetaPartitions] has duplicate vol[%v],vol.ID[%v],mpv.VolID[%v]", mpv.VolName, vol.ID, mpv.VolID))
			continue
		}
		mp := newMetaPartition(mpv.PartitionID, mpv.Start, mpv.End, vol.mpReplicaNum, mpv.LearnerNum, vol.Name, mpv.VolID)
		mp.setHosts(strings.Split(mpv.Hosts, underlineSeparator))
		mp.setPeers(mpv.Peers)
		mp.setLearners(mpv.Learners)
		mp.OfflinePeerID = mpv.OfflinePeerID
		mp.IsRecover = mpv.IsRecover
		mp.PanicHosts = mpv.PanicHosts
		if mp.IsRecover && len(mp.PanicHosts) > 0 {
			for _, address := range mp.PanicHosts {
				c.putBadMetaPartitions(address, mp.PartitionID)
			}
		}
		if mp.IsRecover && len(mp.PanicHosts) == 0 {
			c.putMigratedMetaPartitions("history", mp.PartitionID)
		}
		vol.addMetaPartition(mp)
		log.LogInfof("action[loadMetaPartitions],vol[%v],mp[%v]", vol.Name, mp.PartitionID)
	}
	return
}

func (c *Cluster) loadDataPartitions() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(dataPartitionPrefix))
	if err != nil {
		err = fmt.Errorf("action[loadDataPartitions],err:%v", err.Error())
		return err
	}
	for _, value := range result {

		dpv := &dataPartitionValue{}
		if err = json.Unmarshal(value, dpv); err != nil {
			err = fmt.Errorf("action[loadDataPartitions],value:%v,unmarshal err:%v", string(value), err)
			return err
		}
		vol, err1 := c.getVol(dpv.VolName)
		if err1 != nil {
			log.LogErrorf("action[loadDataPartitions] err:%v", err1.Error())
			continue
		}
		if vol.ID != dpv.VolID {
			Warn(c.Name, fmt.Sprintf("action[loadDataPartitions] has duplicate vol[%v],vol.ID[%v],mpv.VolID[%v]", dpv.VolName, vol.ID, dpv.VolID))
			continue
		}
		dp := newDataPartition(dpv.PartitionID, dpv.ReplicaNum, dpv.VolName, dpv.VolID)
		dp.Hosts = strings.Split(dpv.Hosts, underlineSeparator)
		dp.Peers = dpv.Peers
		dp.Learners = dpv.Learners
		dp.OfflinePeerID = dpv.OfflinePeerID
		dp.isRecover = dpv.IsRecover
		dp.PanicHosts = dpv.PanicHosts
		for _, rv := range dpv.Replicas {
			if !contains(dp.Hosts, rv.Addr) {
				continue
			}
			dp.afterCreation(rv.Addr, rv.DiskPath, c)
		}
		if dp.isRecover && len(dp.PanicHosts) > 0 {
			for _, address := range dp.PanicHosts {
				c.putBadDataPartitionIDs(nil, address, dp.PartitionID)
			}
		}
		if dp.isRecover && len(dp.PanicHosts) == 0 {
			c.putMigratedDataPartitionIDs(nil, "history", dp.PartitionID)
		}
		vol.dataPartitions.put(dp)
		log.LogInfof("action[loadDataPartitions],vol[%v],dp[%v]", vol.Name, dp.PartitionID)
	}
	return
}

func (c *Cluster) loadTokens() (err error) {
	snapshot := c.fsm.store.RocksDBSnapshot()
	it := c.fsm.store.Iterator(snapshot)
	defer func() {
		it.Close()
		c.fsm.store.ReleaseSnapshot(snapshot)
	}()
	prefixKey := []byte(TokenPrefix)
	it.Seek(prefixKey)
	for ; it.ValidForPrefix(prefixKey); it.Next() {
		encodedKey := it.Key()
		encodedValue := it.Value()
		tv := &TokenValue{}
		if err = json.Unmarshal(encodedValue.Data(), tv); err != nil {
			err = fmt.Errorf("action[loadTokens],value:%v,err:%v", encodedValue.Data(), err)
			return err
		}
		vol, err1 := c.getVol(tv.VolName)
		if err1 != nil {
			// if vol not found,record log and continue
			log.LogErrorf("action[loadTokens] err:%v", err1.Error())
			continue
		}
		token := &bsProto.Token{VolName: tv.VolName, TokenType: tv.TokenType, Value: tv.Value}
		vol.putToken(token)
		encodedKey.Free()
		encodedValue.Free()
		log.LogInfof("action[loadTokens],vol[%v],token[%v]", vol.Name, token.Value)
	}
	return
}

func (c *Cluster) loadRegions() (err error) {
	result, err := c.fsm.store.SeekForPrefix([]byte(regionPrefix))
	if err != nil {
		return fmt.Errorf("action[loadRegions] err:%v", err.Error())
	}
	zoneRegionNameMap := make(map[string]string)
	for _, value := range result {
		rv := &regionValue{}
		if err = json.Unmarshal(value, rv); err != nil {
			return fmt.Errorf("action[loadRegions] unmarshal err:%v", err.Error())
		}
		region := newRegionFromRegionValue(rv)
		if err1 := c.t.putRegion(region); err1 != nil {
			log.LogErrorf("action[loadRegions] region[%v] err[%v]", region.Name, err1)
		}
		regionZones := region.getZones()
		for _, zone := range regionZones {
			zoneRegionNameMap[zone] = region.Name
		}
		log.LogInfof("action[loadRegions], region[%v],zones[%v]", region.Name, regionZones)
	}

	// set region name of zones
	c.t.zoneMap.Range(func(zoneName, value interface{}) bool {
		zone, ok := value.(*Zone)
		if !ok {
			return true
		}
		regionName, ok := zoneRegionNameMap[zone.name]
		if !ok {
			return true
		}
		zone.regionName = regionName
		return true
	})
	return
}
