package datanode

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/repl"
	"github.com/chubaofs/chubaofs/storage"
	"github.com/chubaofs/chubaofs/util/errors"
	"github.com/chubaofs/chubaofs/util/log"
)

type DataPartitionValidateCRCTask struct {
	TaskType   uint8
	addr       string
	extents    map[uint64]storage.ExtentInfoBlock
	LeaderAddr string
	Source     string
}

func NewDataPartitionValidateCRCTask(extentFiles []storage.ExtentInfoBlock, source, leaderAddr string) (task *DataPartitionValidateCRCTask) {
	task = &DataPartitionValidateCRCTask{
		extents:    make(map[uint64]storage.ExtentInfoBlock, len(extentFiles)),
		LeaderAddr: leaderAddr,
		Source:     source,
	}
	for _, extentFile := range extentFiles {
		task.extents[extentFile[storage.FileID]] = extentFile
	}
	return
}

func (dp *DataPartition) runValidateCRC(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("DataPartition(%v) runValidateCRC panic(%v)", dp.partitionID, r)
			log.LogWarnf(msg)
		}
	}()
	if dp.partitionStatus == proto.Unavailable {
		return
	}
	if !dp.isLeader {
		return
	}

	start := time.Now().UnixNano()
	log.LogInfof("action[runValidateCRC] partition(%v) start.", dp.partitionID)
	replicas := dp.getReplicaClone()
	if len(replicas) == 0 {
		log.LogErrorf("action[runValidateCRC] partition(%v) replicas is nil.", dp.partitionID)
		return
	}

	validateCRCTasks := make([]*DataPartitionValidateCRCTask, len(replicas))
	err := dp.buildDataPartitionValidateCRCTask(ctx, validateCRCTasks, replicas)
	if err != nil {
		log.LogErrorf("action[runValidateCRC] partition(%v) err(%v).", dp.partitionID, err)
		return
	}

	dp.validateCRC(validateCRCTasks)
	end := time.Now().UnixNano()
	log.LogWarnf("action[runValidateCRC] partition(%v) finish cost[%vms].", dp.partitionID, (end-start)/int64(time.Millisecond))
}

func (dp *DataPartition) buildDataPartitionValidateCRCTask(ctx context.Context, validateCRCTasks []*DataPartitionValidateCRCTask, replicas []string) (err error) {
	// get the local extent info
	extents, err := dp.getLocalExtentInfoForValidateCRC()
	if err != nil {
		return err
	}
	leaderAddr := replicas[0]
	// new validate crc task for the leader
	validateCRCTasks[0] = NewDataPartitionValidateCRCTask(extents, leaderAddr, leaderAddr)
	validateCRCTasks[0].addr = leaderAddr

	// new validate crc task for the followers
	for index := 1; index < len(replicas); index++ {
		var followerExtents []storage.ExtentInfoBlock
		followerAddr := replicas[index]
		if followerExtents, err = dp.getRemoteExtentInfoForValidateCRC(ctx, followerAddr); err != nil {
			log.LogErrorf("buildDataPartitionValidateCRCTask PartitionID(%v) on(%v) err(%v)", dp.partitionID, followerAddr, err)
			continue
		}
		validateCRCTasks[index] = NewDataPartitionValidateCRCTask(followerExtents, followerAddr, leaderAddr)
		validateCRCTasks[index].addr = followerAddr
	}
	return
}


func (dp *DataPartition) getLocalExtentInfoForValidateCRC() (extents []storage.ExtentInfoBlock, err error) {
	if !dp.ExtentStore().IsFinishLoad(){
		err = storage.PartitionIsLoaddingErr
		return
	}
	extents, _, err = dp.extentStore.GetAllWatermarks(proto.AllExtentType, storage.ExtentFilterForValidateCRC())
	if err != nil {
		err = fmt.Errorf("getLocalExtentInfoForValidateCRC DataPartition(%v) err:%v", dp.partitionID, err)
		return
	}
	return
}

func (dp *DataPartition) getRemoteExtentInfoForValidateCRC(ctx context.Context, target string) (extentFiles []storage.ExtentInfoBlock, err error) {
	var packet = proto.NewPacketToGetAllExtentInfo(ctx, dp.partitionID)
	var conn *net.TCPConn
	if conn, err = gConnPool.GetConnect(target); err != nil {
		err = errors.Trace(err, "get connection failed")
		return
	}
	defer func() {
		gConnPool.PutConnectWithErr(conn, err)
	}()
	if err = packet.WriteToConn(conn, proto.WriteDeadlineTime); err != nil {
		err = errors.Trace(err, "write packet to connection failed")
		return
	}
	var reply = new(repl.Packet)
	reply.SetCtx(ctx)
	if err = reply.ReadFromConn(conn, proto.GetAllWatermarksDeadLineTime); err != nil {
		err = errors.Trace(err, "read reply from connection failed")
		return
	}
	if reply.ResultCode != proto.OpOk {
		err = errors.NewErrorf("reply result code: %v", reply.GetOpMsg())
		return
	}
	if reply.Size%20 != 0 {
		// 合法的data长度与20对齐，每20个字节存储一个Extent信息，[0:8)为FileID，[8:16)为Size，[16:20)为Crc
		err = errors.NewErrorf("illegal result data length: %v", len(reply.Data))
		return
	}
	extentFiles = make([]storage.ExtentInfoBlock, 0, len(reply.Data)/20)
	for index := 0; index < int(reply.Size)/24; index++ {
		var offset = index * 20
		var extentID = binary.BigEndian.Uint64(reply.Data[offset:])
		var size = binary.BigEndian.Uint64(reply.Data[offset+8:])
		var crc = binary.BigEndian.Uint32(reply.Data[offset+16:])
		eiBlock := storage.ExtentInfoBlock{
			storage.FileID: extentID,
			storage.Size:   size,
			storage.Crc:    uint64(crc),
		}
		extentFiles = append(extentFiles, eiBlock)
	}
	return
}

func (dp *DataPartition) validateCRC(validateCRCTasks []*DataPartitionValidateCRCTask) {
	if len(validateCRCTasks) <= 1 {
		return
	}
	var (
		extentInfo          storage.ExtentInfoBlock
		ok                  bool
		extentReplicaInfos  []storage.ExtentInfoBlock
		extentReplicaSource map[int]string
		extentCrcInfo       *proto.ExtentCrcInfo
		crcNotEqual         bool
		extentCrcResults    []*proto.ExtentCrcInfo
	)
	for extentID, localExtentInfo := range validateCRCTasks[0].extents {
		extentReplicaInfos = make([]storage.ExtentInfoBlock, 0, len(validateCRCTasks))
		extentReplicaSource = make(map[int]string, len(validateCRCTasks))
		extentReplicaInfos = append(extentReplicaInfos, localExtentInfo)
		extentReplicaSource[0] = validateCRCTasks[0].Source
		for i := 1; i < len(validateCRCTasks); i++ {
			extentInfo, ok = validateCRCTasks[i].extents[extentID]
			if !ok {
				continue
			}
			extentReplicaInfos = append(extentReplicaInfos, extentInfo)
			extentReplicaSource[len(extentReplicaInfos)-1] = validateCRCTasks[i].Source
		}
		if storage.IsTinyExtent(extentID) {
			extentCrcInfo, crcNotEqual = dp.checkTinyExtentFile(extentReplicaInfos, extentReplicaSource)
		} else {
			extentCrcInfo, crcNotEqual = dp.checkNormalExtentFile(extentReplicaInfos, extentReplicaSource)
		}
		if crcNotEqual {
			extentCrcResults = append(extentCrcResults, extentCrcInfo)
		}
	}

	if len(extentCrcResults) != 0 {
		dpCrcInfo := proto.DataPartitionExtentCrcInfo{
			PartitionID:    dp.partitionID,
			ExtentCrcInfos: extentCrcResults,
		}
		if err := MasterClient.NodeAPI().DataNodeValidateCRCReport(&dpCrcInfo); err != nil {
			log.LogErrorf("report DataPartition Validate CRC result failed,PartitionID(%v) err:%v", dp.partitionID, err)
			return
		}
	}
	return
}

func (dp *DataPartition) checkTinyExtentFile(extentInfos []storage.ExtentInfoBlock, extentReplicaSource map[int]string) (extentCrcInfo *proto.ExtentCrcInfo, crcNotEqual bool) {
	if len(extentInfos) <= 1 {
		return
	}
	if !needCrcRepair(extentInfos) {
		return
	}
	if !hasSameSize(extentInfos) {
		sb := new(strings.Builder)
		sb.WriteString(fmt.Sprintf("checkTinyExtentFileErr size not match, dpID[%v] FileID[%v] ", dp.partitionID, extentInfos[0][storage.FileID]))
		for i, extentInfo := range extentInfos {
			sb.WriteString(fmt.Sprintf("fm[%v]:size[%v] ", extentReplicaSource[i], extentInfo[storage.Size]))
		}
		log.LogWarn(sb.String())
		return
	}
	extentCrcInfo, crcNotEqual = getExtentCrcInfo(extentInfos, extentReplicaSource)
	return
}

func (dp *DataPartition) checkNormalExtentFile(extentInfos []storage.ExtentInfoBlock, extentReplicaSource map[int]string) (extentCrcInfo *proto.ExtentCrcInfo, crcNotEqual bool) {
	if len(extentInfos) <= 1 {
		return
	}
	if !needCrcRepair(extentInfos) {
		return
	}
	extentCrcInfo, crcNotEqual = getExtentCrcInfo(extentInfos, extentReplicaSource)
	return
}

func needCrcRepair(extentInfos []storage.ExtentInfoBlock) (needCheckCrc bool) {
	if len(extentInfos) <= 1 {
		return
	}
	baseCrc := extentInfos[0][storage.Crc]
	for _, extentInfo := range extentInfos {
		if extentInfo[storage.Crc] == 0 || extentInfo[storage.Crc] == EmptyCrcValue {
			return
		}
		if extentInfo[storage.Crc] != baseCrc {
			needCheckCrc = true
			return
		}
	}
	return
}

func hasSameSize(extentInfos []storage.ExtentInfoBlock) (same bool) {
	same = true
	if len(extentInfos) <= 1 {
		return
	}
	baseSize := extentInfos[0][storage.Size]
	for _, extentInfo := range extentInfos {
		if extentInfo[storage.Size] != baseSize {
			same = false
			return
		}
	}
	return
}

func getExtentCrcInfo(extentInfos []storage.ExtentInfoBlock, extentReplicaSource map[int]string) (extentCrcInfo *proto.ExtentCrcInfo, crcNotEqual bool) {
	if len(extentInfos) <= 1 {
		return
	}
	crcLocAddrMap := make(map[uint64][]string)
	for i, extentInfo := range extentInfos {
		crcLocAddrMap[extentInfo[storage.Crc]] = append(crcLocAddrMap[extentInfo[storage.Crc]], extentReplicaSource[i])
	}
	if len(crcLocAddrMap) <= 1 {
		return
	}
	crcNotEqual = true
	extentCrcInfo = &proto.ExtentCrcInfo{
		FileID:        extentInfos[0][storage.FileID],
		ExtentNum:     len(extentInfos),
		CrcLocAddrMap: crcLocAddrMap,
	}
	return
}
