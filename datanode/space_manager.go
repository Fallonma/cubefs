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
	"fmt"
	"math"
	"math/rand"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"math"
	"os"

	syslog "log"

	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/raftstore"
	"github.com/cubefs/cubefs/util"
	"github.com/cubefs/cubefs/util/atomicutil"
	"github.com/cubefs/cubefs/util/loadutil"
	"github.com/cubefs/cubefs/util/log"
	"github.com/shirou/gopsutil/disk"
)

const DefaultStopDpLimit = DefaultCurrentLoadDpLimit

// SpaceManager manages the disk space.
type SpaceManager struct {
	clusterID            string
	disks                map[string]*Disk
	partitions           map[uint64]*DataPartition
	raftStore            raftstore.RaftStore
	nodeID               uint64
	diskMutex            sync.RWMutex
	partitionMutex       sync.RWMutex
	stats                *Stats
	stopC                chan bool
	selectedIndex        int // TODO what is selected index
	diskList             []string
	dataNode             *DataNode
	createPartitionMutex sync.RWMutex
	rand                 *rand.Rand
	currentLoadDpCount   int
	currentStopDpCount   int
	diskUtils            map[string]*atomicutil.Float64
	samplerDone          chan struct{}
	allDisksLoaded       bool
}

const diskSampleDuration = 1 * time.Second

// NewSpaceManager creates a new space manager.
func NewSpaceManager(dataNode *DataNode) *SpaceManager {
	space := &SpaceManager{}
	space.disks = make(map[string]*Disk)
	space.diskList = make([]string, 0)
	space.partitions = make(map[uint64]*DataPartition)
	space.stats = NewStats(dataNode.zoneName)
	space.stopC = make(chan bool)
	space.dataNode = dataNode
	space.rand = rand.New(rand.NewSource(time.Now().Unix()))
	space.currentLoadDpCount = DefaultCurrentLoadDpLimit
	space.currentStopDpCount = DefaultStopDpLimit
	space.diskUtils = make(map[string]*atomicutil.Float64)
	go space.statUpdateScheduler()

	return space
}

func (manager *SpaceManager) SetCurrentLoadDpLimit(limit int) {
	if limit != 0 {
		manager.currentLoadDpCount = limit
	}
}

func (manager *SpaceManager) SetCurrentStopDpLimit(limit int) {
	if limit != 0 {
		manager.currentStopDpCount = limit
	}
}

func (manager *SpaceManager) Stop() {
	begin := time.Now()
	defer func() {
		msg := fmt.Sprintf("[Stop] stop space manager using time(%v)", time.Since(begin))
		log.LogInfo(msg)
		syslog.Print(msg)
	}()
	defer func() {
		recover()
	}()
	close(manager.stopC)
	// stop sampler
	close(manager.samplerDone)
	// Parallel stop data partitions.
	const maxParallelism = 128
	parallelism := int(math.Min(float64(maxParallelism), float64(len(manager.partitions))))
	wg := sync.WaitGroup{}
	partitionC := make(chan *DataPartition, parallelism)
	wg.Add(1)

	// Close raft store.
	for _, partition := range manager.partitions {
		partition.stopRaft()
	}

	limitCh := make(map[string]chan interface{})
	for _, partition := range manager.partitions {
		_, ok := limitCh[partition.Disk().Path]
		if !ok {
			ch := make(chan interface{}, manager.currentStopDpCount)
			defer func() {
				close(ch)
			}()
			limitCh[partition.Disk().Path] = ch
		}
	}

	go func(c chan<- *DataPartition) {
		defer wg.Done()
		for _, partition := range manager.partitions {
			c <- partition
		}
		close(c)
	}(partitionC)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func(c <-chan *DataPartition) {
			defer wg.Done()
			var partition *DataPartition
			for {
				if partition = <-c; partition == nil {
					return
				}
				func() {
					limit := limitCh[partition.Disk().Path]
					defer func() {
						<-limit
					}()
					limit <- 1
					partition.Stop()
				}()
			}
		}(partitionC)
	}
	wg.Wait()
}

func (manager *SpaceManager) GetAllDiskPartitions() []*disk.PartitionStat {
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	partitions := make([]*disk.PartitionStat, 0, len(manager.disks))
	for _, disk := range manager.disks {
		partition := disk.GetDiskPartition()
		if partition != nil {
			partitions = append(partitions, partition)
		}
	}
	return partitions
}

func (manager *SpaceManager) FillIoUtils(samples map[string]loadutil.DiskIoSample) {
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	for _, sample := range samples {
		util := manager.diskUtils[sample.GetPartition().Device]
		if util != nil {
			util.Store(sample.GetIoUtilPercent())
		}
	}
}

func (manager *SpaceManager) StartDiskSample() {
	manager.samplerDone = make(chan struct{})
	go func() {
		for {
			select {
			case <-manager.samplerDone:
				return
			default:
				partitions := manager.GetAllDiskPartitions()
				samples, err := loadutil.GetDisksIoSample(partitions, diskSampleDuration)
				if err != nil {
					log.LogErrorf("failed to sample disk %v\n", err.Error())
					return
				}
				manager.FillIoUtils(samples)
			}
		}
	}()
}

func (manager *SpaceManager) GetDiskUtils() map[string]float64 {
	utils := make(map[string]float64)
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	for device, used := range manager.diskUtils {
		utils[device] = used.Load()
	}
	return utils
}

func (manager *SpaceManager) GetDiskUtil(disk *Disk) (util float64) {
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	util = manager.diskUtils[disk.diskPartition.Device].Load()
	return
}

func (manager *SpaceManager) SetNodeID(nodeID uint64) {
	manager.nodeID = nodeID
}

func (manager *SpaceManager) GetNodeID() (nodeID uint64) {
	return manager.nodeID
}

func (manager *SpaceManager) SetClusterID(clusterID string) {
	manager.clusterID = clusterID
}

func (manager *SpaceManager) GetClusterID() (clusterID string) {
	return manager.clusterID
}

func (manager *SpaceManager) SetRaftStore(raftStore raftstore.RaftStore) {
	manager.raftStore = raftStore
}

func (manager *SpaceManager) GetRaftStore() (raftStore raftstore.RaftStore) {
	return manager.raftStore
}

func (manager *SpaceManager) RangePartitions(f func(partition *DataPartition) bool) {
	if f == nil {
		return
	}
	manager.partitionMutex.RLock()
	partitions := make([]*DataPartition, 0)
	for _, dp := range manager.partitions {
		partitions = append(partitions, dp)
	}
	manager.partitionMutex.RUnlock()

	for _, partition := range partitions {
		if !f(partition) {
			break
		}
	}
}

func (manager *SpaceManager) GetDisks() (disks []*Disk) {
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	disks = make([]*Disk, 0)
	for _, disk := range manager.disks {
		disks = append(disks, disk)
	}
	return
}

func (manager *SpaceManager) Stats() *Stats {
	return manager.stats
}

func (manager *SpaceManager) LoadDisk(path string, reservedSpace, diskRdonlySpace uint64, maxErrCnt int,
	diskEnableReadRepairExtentLimit bool,
) (err error) {
	var (
		disk    *Disk
		visitor PartitionVisitor
	)

	if diskRdonlySpace < reservedSpace {
		diskRdonlySpace = reservedSpace
	}

	log.LogDebugf("action[LoadDisk] load disk from path(%v).", path)
	visitor = func(dp *DataPartition) {
		manager.partitionMutex.Lock()
		defer manager.partitionMutex.Unlock()
		if _, has := manager.partitions[dp.partitionID]; !has {
			manager.partitions[dp.partitionID] = dp
			log.LogDebugf("action[LoadDisk] put partition(%v) to manager manager.", dp.partitionID)
		}
	}

	if _, err = manager.GetDisk(path); err != nil {
		disk, err = NewDisk(path, reservedSpace, diskRdonlySpace, maxErrCnt, manager, diskEnableReadRepairExtentLimit)
		if err != nil {
			log.LogErrorf("NewDisk fail err:[%v]", err)
			return
		}
		err = disk.RestorePartition(visitor)
		if err != nil {
			log.LogErrorf("RestorePartition fail err:[%v]", err)
			return
		}
		manager.putDisk(disk)
		err = nil
		go disk.doBackendTask()
	}
	return
}

func (manager *SpaceManager) GetDisk(path string) (d *Disk, err error) {
	manager.diskMutex.RLock()
	defer manager.diskMutex.RUnlock()
	disk, has := manager.disks[path]
	if has && disk != nil {
		d = disk
		return
	}
	err = fmt.Errorf("disk(%v) not exsit", path)
	return
}

func (manager *SpaceManager) putDisk(d *Disk) {
	manager.diskMutex.Lock()
	manager.disks[d.Path] = d
	manager.diskList = append(manager.diskList, d.Path)
	if d.GetDiskPartition() != nil {
		manager.diskUtils[d.GetDiskPartition().Device] = &atomicutil.Float64{}
		manager.diskUtils[d.GetDiskPartition().Device].Store(0)
	}
	manager.diskMutex.Unlock()
}

func (manager *SpaceManager) updateMetrics() {
	manager.diskMutex.RLock()
	var (
		total, used, available                                 uint64
		totalPartitionSize, remainingCapacityToCreatePartition uint64
		maxCapacityToCreatePartition, partitionCnt             uint64
	)
	maxCapacityToCreatePartition = 0
	for _, d := range manager.disks {
		if d.Status == proto.Unavailable {
			log.LogInfof("disk is broken, not stat disk useage, diskpath %s", d.Path)
			continue
		}

		if d.Used > d.Total {
			total += d.Used
		} else {
			total += d.Total
		}

		used += d.Used
		available += d.Available
		totalPartitionSize += d.Allocated
		remainingCapacityToCreatePartition += d.Unallocated
		partitionCnt += uint64(d.PartitionCount())
		if maxCapacityToCreatePartition < d.Unallocated {
			maxCapacityToCreatePartition = d.Unallocated
		}
	}
	manager.diskMutex.RUnlock()
	log.LogDebugf("action[updateMetrics] total(%v) used(%v) available(%v) totalPartitionSize(%v)  remainingCapacityToCreatePartition(%v) "+
		"partitionCnt(%v) maxCapacityToCreatePartition(%v) ", total, used, available, totalPartitionSize, remainingCapacityToCreatePartition, partitionCnt, maxCapacityToCreatePartition)
	manager.stats.updateMetrics(total, used, available, totalPartitionSize,
		remainingCapacityToCreatePartition, maxCapacityToCreatePartition, partitionCnt)
}

const DiskSelectMaxStraw = 65536

func (manager *SpaceManager) selectDisk(decommissionedDisks []string) (d *Disk) {
	manager.diskMutex.Lock()
	defer manager.diskMutex.Unlock()
	decommissionedDiskMap := make(map[string]struct{})
	for _, disk := range decommissionedDisks {
		decommissionedDiskMap[disk] = struct{}{}
	}
	maxStraw := float64(0)
	for _, disk := range manager.disks {
		if _, ok := decommissionedDiskMap[disk.Path]; ok {
			log.LogInfof("action[minPartitionCnt] exclude decommissioned disk[%v]", disk.Path)
			continue
		}
		if disk.Status != proto.ReadWrite {
			log.LogInfof("[minPartitionCnt] disk(%v) is not writable", disk.Path)
			continue
		}

		straw := float64(manager.rand.Intn(DiskSelectMaxStraw))
		straw = math.Log(straw/float64(DiskSelectMaxStraw)) / (float64(atomic.LoadUint64(&disk.Available)) / util.GB)
		if d == nil || straw > maxStraw {
			maxStraw = straw
			d = disk
		}
	}
	if d != nil && d.Status != proto.ReadWrite {
		d = nil
		return
	}
	return d
}

func (manager *SpaceManager) statUpdateScheduler() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ticker.C:
				manager.updateMetrics()
			case <-manager.stopC:
				ticker.Stop()
				return
			}
		}
	}()
}

func (manager *SpaceManager) Partition(partitionID uint64) (dp *DataPartition) {
	manager.partitionMutex.RLock()
	defer manager.partitionMutex.RUnlock()
	dp = manager.partitions[partitionID]
	return
}

func (manager *SpaceManager) AttachPartition(dp *DataPartition) {
	begin := time.Now()
	defer func() {
		log.LogInfof("[AttachPartition] load dp(%v) attach using time(%v)", dp.partitionID, time.Now().Sub(begin))
	}()
	manager.partitionMutex.Lock()
	defer manager.partitionMutex.Unlock()
	manager.partitions[dp.partitionID] = dp
}

// DetachDataPartition removes a data partition from the partition map.
func (manager *SpaceManager) DetachDataPartition(partitionID uint64) {
	manager.partitionMutex.Lock()
	defer manager.partitionMutex.Unlock()
	delete(manager.partitions, partitionID)
}

func (manager *SpaceManager) CreatePartition(request *proto.CreateDataPartitionRequest) (dp *DataPartition, err error) {
	manager.partitionMutex.Lock()
	defer manager.partitionMutex.Unlock()
	dpCfg := &dataPartitionCfg{
		PartitionID:   request.PartitionId,
		VolName:       request.VolumeId,
		Peers:         request.Members,
		Hosts:         request.Hosts,
		RaftStore:     manager.raftStore,
		NodeID:        manager.nodeID,
		ClusterID:     manager.clusterID,
		PartitionSize: request.PartitionSize,
		PartitionType: int(request.PartitionTyp),
		ReplicaNum:    request.ReplicaNum,
		VerSeq:        request.VerSeq,
		CreateType:    request.CreateType,
		Forbidden:     false,
	}
	log.LogInfof("action[CreatePartition] dp %v dpCfg.Peers %v request.Members %v",
		dpCfg.PartitionID, dpCfg.Peers, request.Members)
	dp = manager.partitions[dpCfg.PartitionID]
	if dp != nil {
		if err = dp.IsEqualCreateDataPartitionRequest(request); err != nil {
			return nil, err
		}
		return
	}
	disk := manager.selectDisk(request.DecommissionedDisks)
	if disk == nil {
		log.LogErrorf("[CreatePartition] dp(%v) failed to select disk", dpCfg.PartitionID)
		return nil, ErrNoSpaceToCreatePartition
	}
	if dp, err = CreateDataPartition(dpCfg, disk, request); err != nil {
		return
	}
	manager.partitions[dp.partitionID] = dp
	return
}

// DeletePartition deletes a partition based on the partition id.
func (manager *SpaceManager) DeletePartition(dpID uint64) {
	manager.partitionMutex.Lock()

	dp := manager.partitions[dpID]
	if dp == nil {
		manager.partitionMutex.Unlock()
		// maybe dp not loaded when triggered disk error, need to remove disk root dir
		manager.deleteDataPartitionNotLoaded(dpID)
		return
	}

	delete(manager.partitions, dpID)
	manager.partitionMutex.Unlock()
	dp.Stop()
	dp.Disk().DetachDataPartition(dp)
	if err := dp.RemoveAll(); err != nil {
		log.LogErrorf("[DeletePartition] failed to remove dp(%v) dir(%v), err(%v)", dp.partitionID, dp.Path(), err)
	}
}

func (s *DataNode) buildHeartBeatResponse(response *proto.DataNodeHeartbeatResponse) {
	response.Status = proto.TaskSucceeds
	stat := s.space.Stats()
	stat.Lock()
	response.Used = stat.Used
	response.Total = stat.Total
	response.Available = stat.Available
	response.CreatedPartitionCnt = uint32(stat.CreatedPartitionCnt)
	response.TotalPartitionSize = stat.TotalPartitionSize
	response.MaxCapacity = stat.MaxCapacityToCreatePartition
	response.RemainingCapacity = stat.RemainingCapacityToCreatePartition
	response.BadDisks = make([]string, 0)
	response.DiskStats = make([]proto.DiskStat, 0)
	response.StartTime = s.startTime
	stat.Unlock()

	response.ZoneName = s.zoneName
	response.PartitionReports = make([]*proto.DataPartitionReport, 0)
	space := s.space
	space.RangePartitions(func(partition *DataPartition) bool {
		leaderAddr, isLeader := partition.IsRaftLeader()
		vr := &proto.DataPartitionReport{
			VolName:                    partition.volumeID,
			PartitionID:                uint64(partition.partitionID),
			PartitionStatus:            partition.Status(),
			Total:                      uint64(partition.Size()),
			Used:                       uint64(partition.Used()),
			DiskPath:                   partition.Disk().Path,
			IsLeader:                   isLeader,
			ExtentCount:                partition.GetExtentCount(),
			NeedCompare:                true,
			DecommissionRepairProgress: partition.decommissionRepairProgress,
			LocalPeers:                 partition.config.Peers,
			TriggerDiskError:           atomic.LoadUint64(&partition.diskErrCnt) > 0,
		}
		log.LogDebugf("action[Heartbeats] dpid(%v), status(%v) total(%v) used(%v) leader(%v) isLeader(%v) TriggerDiskError(%v).",
			vr.PartitionID, vr.PartitionStatus, vr.Total, vr.Used, leaderAddr, vr.IsLeader, vr.TriggerDiskError)
		response.PartitionReports = append(response.PartitionReports, vr)
		return true
	})

	disks := space.GetDisks()
	for _, d := range disks {
		response.AllDisks = append(response.AllDisks, d.Path)
		brokenDpsCnt := d.GetDiskErrPartitionCount()
		brokenDps := d.GetDiskErrPartitionList()
		log.LogInfof("[buildHeartBeatResponse] disk(%v) status(%v) broken dp len(%v)", d.Path, d.Status, brokenDpsCnt)
		if d.Status == proto.Unavailable || brokenDpsCnt != 0 {
			response.BadDisks = append(response.BadDisks, d.Path)
			bds := proto.BadDiskStat{
				DiskPath:             d.Path,
				TotalPartitionCnt:    d.PartitionCount(),
				DiskErrPartitionList: brokenDps,
			}
			response.BadDiskStats = append(response.BadDiskStats, bds)
			log.LogErrorf("[buildHeartBeatResponse] disk(%v) total(%v) broken dp len(%v) %v",
				d.Path, bds.TotalPartitionCnt, brokenDpsCnt, brokenDps)
		}

		bds := proto.DiskStat{
			Status:            d.Status,
			DiskPath:          d.Path,
			Total:             d.Total,
			Used:              d.Used,
			Available:         d.Available,
			IOUtil:            d.space.GetDiskUtil(d),
			TotalPartitionCnt: d.PartitionCount(),

			DiskErrPartitionList: d.GetDiskErrPartitionList(),
		}
		response.DiskStats = append(response.DiskStats, bds)
	}
}

func (manager *SpaceManager) deleteDataPartitionNotLoaded(id uint64) {
	disks := manager.GetDisks()
	for _, d := range disks {
		if d.HasDiskErrPartition(id) {
			// delete it from DiskErrPartitionSet, not report to master any more
			d.DiskErrPartitionSet.Delete(id)
			// remove dp root dir
			fileInfoList, err := os.ReadDir(d.Path)
			if err != nil {
				log.LogErrorf("[deleteDataPartitionNotLoaded] disk(%v)load file list err %v",
					d.Path, err)
				return
			}
			for _, fileInfo := range fileInfoList {
				filename := fileInfo.Name()
				if !d.isPartitionDir(filename) {
					log.LogWarnf("[deleteDataPartitionNotLoaded] disk(%v)ignore file %v",
						d.Path, filename)
					continue
				}

				if partitionID, _, err := unmarshalPartitionName(filename); err != nil {
					log.LogErrorf("action[deleteDataPartitionNotLoaded] unmarshal partitionName(%v) from disk(%v) err(%v) ",
						filename, d.Path, err.Error())
					continue
				} else {
					if partitionID == id {
						rootPath := path.Join(d.Path, filename)
						err = os.RemoveAll(rootPath)
						if err != nil {
							log.LogErrorf("action[deleteDataPartitionNotLoaded] disk(%v) remove root dir (%v) failed err(%v) ",
								d.Path, rootPath, err.Error())
						}
					}
				}
			}
		}
	}
}
