package master

import (
	"fmt"
	"github.com/cubefs/cubefs/proto"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestMetaPartition(t *testing.T) {
	server.cluster.doCheckDataNodeHeartbeat()
	server.cluster.doCheckMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	server.cluster.doCheckMetaPartitions()
	commonVol, err := server.cluster.getVol(commonVolName)
	if !assert.NoError(t, err) {
		return
	}
	createMetaPartition(commonVol, t)
	maxPartitionID := commonVol.maxPartitionID()
	getMetaPartition(commonVol.Name, maxPartitionID, t)
	getMetaPartition("", maxPartitionID, t) //empty vol name
	loadMetaPartitionTest(commonVol, maxPartitionID, t)
	server.cluster.doCheckMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	decommissionMetaPartition(commonVol, maxPartitionID, t)
	decommissionMetaPartitionWithoutReplica(commonVol, maxPartitionID, t)
	decommissionMetaPartitionToDestAddr(commonVol, maxPartitionID, t)
	setMetaPartitionIsRecover(commonVol, maxPartitionID, true, t)
	setMetaPartitionIsRecover(commonVol, maxPartitionID, false, t)
}

func createMetaPartition(vol *Vol, t *testing.T) {
	server.cluster.DisableAutoAllocate = false
	maxPartitionID := commonVol.maxPartitionID()
	mp, err := commonVol.metaPartition(maxPartitionID)
	if !assert.NoError(t, err) {
		return
	}
	var start uint64
	start = mp.Start + proto.DefaultMetaPartitionInodeIDStep
	reqURL := fmt.Sprintf("%v%v?name=%v&start=%v",
		hostAddr, proto.AdminCreateMetaPartition, vol.Name, start)
	process(reqURL, t)
	if start < mp.MaxInodeID {
		start = mp.MaxInodeID
	}
	step := proto.DefaultMetaPartitionInodeIDStep
	if commonVol.MpSplitStep != 0 {
		step = commonVol.MpSplitStep
	}
	start = start + step
	vol, err = server.cluster.getVol(vol.Name)
	if !assert.NoError(t, err) {
		return
	}
	maxPartitionID = vol.maxPartitionID()
	mp, err = vol.metaPartition(maxPartitionID)
	if !assert.NoError(t, err) {
		return
	}
	start = start + 1
	if !assert.Equalf(t, mp.Start, start, "expect start[%v],mp.start[%v],not equal", start, mp.Start) {
		return
	}
}

func getMetaPartition(volName string, id uint64, t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?name=%v&id=%v",
		hostAddr, proto.ClientMetaPartition, volName, id)
	process(reqURL, t)
}

func loadMetaPartitionTest(vol *Vol, id uint64, t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?name=%v&id=%v", hostAddr, proto.AdminLoadMetaPartition, vol.Name, id)
	process(reqURL, t)
}

func decommissionMetaPartition(vol *Vol, id uint64, t *testing.T) {
	server.cluster.doCheckMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.AdminGetCluster)
	process(reqURL, t)
	vol, err := server.cluster.getVol(vol.Name)
	if !assert.NoError(t, err) {
		return
	}
	mp, err := vol.metaPartition(id)
	if !assert.NoErrorf(t, err, "decommissionMetaPartition,err [%v]", err) {
		return
	}
	offlineAddr := mp.Hosts[0]
	reqURL = fmt.Sprintf("%v%v?name=%v&id=%v&addr=%v&force=true",
		hostAddr, proto.AdminDecommissionMetaPartition, vol.Name, id, offlineAddr)
	process(reqURL, t)
	mp, err = server.cluster.getMetaPartitionByID(id)
	if !assert.NoErrorf(t, err, "decommissionMetaPartition,err [%v]", err) {
		return
	}
	if !assert.NotContainsf(t, mp.Hosts, offlineAddr, "decommissionMetaPartition failed,offlineAddr[%v],hosts[%v]", offlineAddr, mp.Hosts) {
		return
	}
}

func decommissionMetaPartitionWithoutReplica(vol *Vol, id uint64, t *testing.T) {
	server.cluster.doCheckMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.AdminGetCluster)
	process(reqURL, t)
	vol, err := server.cluster.getVol(vol.Name)
	if !assert.NoError(t, err) {
		return
	}
	mp, err := vol.metaPartition(id)
	if !assert.NoErrorf(t, err, "decommissionMetaPartition,err [%v]", err) {
		return
	}
	mp.RLock()
	offlineAddr := mp.Hosts[0]
	for _, replica := range mp.Replicas {
		if replica.Addr == offlineAddr {
			mp.removeReplicaByAddr(offlineAddr)
		}
	}
	mp.IsRecover = false
	mp.RUnlock()
	reqURL = fmt.Sprintf("%v%v?name=%v&id=%v&addr=%v&force=true",
		hostAddr, proto.AdminDecommissionMetaPartition, vol.Name, id, offlineAddr)
	process(reqURL, t)
	mp, err = server.cluster.getMetaPartitionByID(id)
	if !assert.NoErrorf(t, err, "decommissionMetaPartition,err [%v]", err) {
		return
	}
	if !assert.NotContainsf(t, mp.Hosts, offlineAddr, "decommissionMetaPartition failed,offlineAddr[%v],hosts[%v]", offlineAddr, mp.Hosts) {
		return
	}

	if len(mp.Hosts) == 2 || len(mp.Replicas) == 2 {
		t.Errorf("mp decommissionWithoutReplica failed,hosts[%v],replicas[%v]", len(mp.Hosts), len(mp.Replicas))
		return
	}
	mp.IsRecover = false
}

func decommissionMetaPartitionToDestAddr(vol *Vol, id uint64, t *testing.T) {
	msAddr := mms10Addr
	addMetaServer(msAddr, testZone2)
	server.cluster.doCheckMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	vol, err := server.cluster.getVol(vol.Name)
	if !assert.NoError(t, err) {
		return
	}
	mp, err := vol.metaPartition(id)
	if !assert.NoErrorf(t, err, "decommissionMetaPartition,err [%v]", err) {
		return
	}
	mp.IsRecover = false
	offlineAddr := mp.Hosts[len(mp.Hosts)-1]
	reqURL := fmt.Sprintf("%v%v?name=%v&id=%v&addr=%v&destAddr=%v&force=true",
		hostAddr, proto.AdminDecommissionMetaPartition, mp.volName, mp.PartitionID, offlineAddr, msAddr)
	process(reqURL, t)
	if contains(mp.Hosts, offlineAddr) || !contains(mp.Hosts, msAddr) {
		t.Errorf("decommissionMetaPartitionToDestAddr failed,offlineAddr[%v],destAddr[%v],hosts[%v]", offlineAddr, msAddr, mp.Hosts)
		return
	}
}

func setMetaPartitionIsRecover(vol *Vol, id uint64, isRecover bool, t *testing.T) {
	mp, err := vol.metaPartition(id)
	if !assert.NoErrorf(t, err, "setMetaPartitionIsRecover,err [%v]", err) {
		return
	}
	reqURL := fmt.Sprintf("%v%v?id=%v&isRecover=%v",
		hostAddr, proto.AdminMetaPartitionSetIsRecover, mp.PartitionID, isRecover)
	process(reqURL, t)
	if !assert.Equalf(t, isRecover, mp.IsRecover, "expect isRecover[%v],mp.isRecover[%v],not equal", isRecover, mp.IsRecover) {
		return
	}
}
