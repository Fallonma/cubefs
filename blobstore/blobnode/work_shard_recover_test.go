// Copyright 2022 The CubeFS Authors.
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

package blobnode

import (
	"bytes"
	"context"
	"hash/crc32"
	"log"
	"math"
	"testing"

	"github.com/cubefs/cubefs/blobstore/blobnode/base/workutils"
	"github.com/cubefs/cubefs/blobstore/common/codemode"
	errcode "github.com/cubefs/cubefs/blobstore/common/errors"
	"github.com/cubefs/cubefs/blobstore/common/proto"
	"github.com/cubefs/cubefs/blobstore/util/errors"
	"github.com/stretchr/testify/require"
)

func TestShardsBuf(t *testing.T) {
	taskBufPool := workutils.NewBufPool(&workutils.BufConfig{
		MigrateBufSize:     1024 * 1024,
		MigrateBufCapacity: 20,
		RepairBufSize:      0,
		RepairBufCapacity:  0,
	})
	var bidInfos []*ShardInfoSimple
	bids := []proto.BlobID{1, 2, 3}
	sizes := []int64{1024, 2048, 0}
	bufs := make([][]byte, 10)
	for idx := range bids {
		buf := genMockBytes('a', sizes[idx])
		bufs[idx] = buf
		ele := ShardInfoSimple{
			Bid:  bids[idx],
			Size: sizes[idx],
		}
		bidInfos = append(bidInfos, &ele)
	}
	buf, _ := taskBufPool.GetMigrateBuf()
	shardsBuf := NewShardsBuf(buf)
	err := shardsBuf.PlanningDataLayout(bidInfos)
	require.NoError(t, err)
	for idx := range bids {
		buf, err := shardsBuf.getShardBuf(bids[idx])
		require.NoError(t, err)
		require.Equal(t, 0, len(buf))
	}

	_, err = shardsBuf.FetchShard(1)
	require.EqualError(t, errcode.ErrShardPartialRepairFailed, err.Error())

	err = shardsBuf.PutShard(1, bytes.NewReader(genMockBytes('a', 1023)))
	require.Error(t, err)

	err = shardsBuf.PutShard(1, bytes.NewReader(genMockBytes('a', 1024)))
	require.NoError(t, err)

	buf, err = shardsBuf.FetchShard(1)
	require.NoError(t, err)
	require.Equal(t, 1024, len(buf))

	_, err = shardsBuf.FetchShard(4)
	require.Error(t, err)

	err = shardsBuf.PutShard(3, bytes.NewReader(genMockBytes('a', 1024)))
	require.NoError(t, err)

	buf, err = shardsBuf.FetchShard(3)
	require.NoError(t, err)
	require.Equal(t, 0, len(buf))

	ok := shardsBuf.shardIsOk(3)
	require.Equal(t, true, ok)
}

func InitMockRepair(mode codemode.CodeMode) (*ShardRecover, []*ShardInfoSimple, *MockGetter, VunitLocations) {
	workutils.TaskBufPool = workutils.NewBufPool(&workutils.BufConfig{
		MigrateBufSize:     10 * 1024,
		MigrateBufCapacity: 100,
		RepairBufSize:      10 * 1024,
		RepairBufCapacity:  100,
	})
	replicas := genMockVol(1, mode)
	getter := NewMockGetter(replicas, mode)

	bidInfos := []*ShardInfoSimple{}
	bids := getter.getBids()
	sizes := getter.getSizes()
	for idx := range bids {
		ele := ShardInfoSimple{
			Bid:  bids[idx],
			Size: sizes[idx],
		}
		bidInfos = append(bidInfos, &ele)
	}

	repair := NewShardRecover(replicas, mode, bidInfos, getter, 3, proto.TaskTypeShardRepair, false)
	return repair, bidInfos, getter, replicas
}

func TestDirectGetShardAllMode(t *testing.T) {
	testWithAllMode(t, testDirectGetShard)
}

func testDirectGetShard(t *testing.T, mode codemode.CodeMode) {
	repair, bidInfos, getter, replicas := InitMockRepair(mode)
	badi := []uint8{0, 1}

	failBids, err := repair.directGetShard(context.Background(), GetBids(bidInfos), badi)
	require.NoError(t, err)
	require.Equal(t, 0, len(failBids))
	for _, bad := range badi {
		vuid := replicas[bad].Vuid
		for _, bid := range getter.getBids() {
			data, err := repair.GetShard(bad, bid)
			require.NoError(t, err)
			crc1 := crc32.ChecksumIEEE(data)
			crc2 := getter.getShardCrc32(vuid, bid)
			require.Equal(t, crc2, crc1)
		}
	}
	testCheckData(t, repair, getter, badi)
}

func TestRepairByLocalStripeAllMode(t *testing.T) {
	testWithAllMode(t, testRepairByLocalStripe)
}

func testRepairByLocalStripe(t *testing.T, mode codemode.CodeMode) {
	repair, bidInfos, getter, replicas := InitMockRepair(mode)
	modeInfo := mode.Tactic()
	if modeInfo.L == 0 || modeInfo.CodeType == codemode.AzureLrcP1 {
		return
	}

	badi := []uint8{0}
	err := repair.recoverByLocalStripe(context.Background(), GetBids(bidInfos), badi)
	require.NoError(t, err)
	for _, bid := range GetBids(bidInfos) {
		for _, bad := range badi {
			shard, err := repair.chunksShardsBuf[bad].FetchShard(bid)
			require.NoError(t, err)
			expectCrc32 := getter.getShardCrc32(replicas[bad].Vuid, bid)
			recoverShardCrc32 := crc32.ChecksumIEEE(shard)
			require.Equal(t, expectCrc32, recoverShardCrc32, bid, replicas[bad].Vuid)
		}
	}
	testCheckData(t, repair, getter, badi)
}

func TestRepairByGlobalStripeAllMode(t *testing.T) {
	testWithAllMode(t, testRepairByGlobalStripe)
}

func testRepairByGlobalStripe(t *testing.T, mode codemode.CodeMode) {
	repair, bidInfos, getter, replicas := InitMockRepair(mode)
	badi := []uint8{0}
	err := repair.recoverByGlobalStripe(context.Background(), GetBids(bidInfos), badi)
	require.NoError(t, err)
	for _, bid := range GetBids(bidInfos) {
		for _, bad := range badi {
			shard, err := repair.chunksShardsBuf[bad].FetchShard(bid)
			require.NoError(t, err)
			expectCrc32 := getter.getShardCrc32(replicas[bad].Vuid, bid)
			recoverShardCrc32 := crc32.ChecksumIEEE(shard)
			require.Equal(t, expectCrc32, recoverShardCrc32)
		}
	}
	testCheckData(t, repair, getter, badi)
	repair.ReleaseBuf()
}

func TestShardRecover_PartialRepair_RS(t *testing.T) {
	for _, mode := range []codemode.CodeMode{codemode.EC6P6, codemode.EC12P9, codemode.EC15P12} {
		testShardPartialRepair(t, mode)
	}
}

func testShardPartialRepair(t *testing.T, mode codemode.CodeMode) {
	repair, bidInfos, getter, _ := InitMockRepair(mode)
	repair.enablePartial = true
	idxs := repair.locations.Indexes()
	ctx := context.Background()
	err := repair.allocBuf(ctx, idxs)
	require.NoError(t, err)
	badi := []uint8{8}
	err = repair.partialRepairBids(ctx, GetBids(bidInfos), repairStripe{
		locations: repair.locations,
		n:         repair.codeMode.T().N,
		m:         repair.codeMode.T().M,
		badIdxes:  badi,
	})
	require.NoError(t, err)
	testCheckData(t, repair, getter, badi)
	failBids := repair.collectFailBids(GetBids(bidInfos), badi)
	require.Len(t, failBids, 0)
	// repair failed
	repair, bidInfos, getter, _ = InitMockRepair(mode)
	repair.enablePartial = true
	getter.setFail(repair.locations[9].Vuid, errors.New("fake error"))
	getter.setFail(repair.locations[1].Vuid, errors.New("fake error"))
	getter.setFail(repair.locations[2].Vuid, errors.New("fake error"))
	idxs = repair.locations.Indexes()
	err = repair.allocBuf(ctx, idxs)
	require.NoError(t, err)
	err = repair.partialRepairBids(ctx, GetBids(bidInfos), repairStripe{
		locations: repair.locations,
		n:         repair.codeMode.T().N,
		m:         repair.codeMode.T().M,
		badIdxes:  badi,
	})
	require.NoError(t, err)
	failBids = repair.collectFailBids(GetBids(bidInfos), badi)
	require.Len(t, failBids, len(GetBids(bidInfos))-1)
	repair.ReleaseBuf()
}

func TestGetRepairPlan(t *testing.T) {
	repair, _, _, _ := InitMockRepair(codemode.EC6P6)

	dl, pl := repair.getPartialPlan(8, repair.codeMode.T().N+2)
	require.Equal(t, repair.codeMode.T().N+2, len(dl)+len(pl))

	dl, pl = repair.getPartialPlan(8, 12)
	require.NotEqual(t, 12, len(dl)+len(pl))
}

func TestRecoverLocalReplicaShards(t *testing.T) {
	repair, bidInfos, getter, _ := InitMockRepair(codemode.EC6P10L2)
	badi := []uint8{16}
	for idx := range [11]struct{}{} {
		getter.setFail(repair.locations[idx].Vuid, errors.New("fake error"))
	}

	err := repair.recoverReplicaShards(context.Background(), badi, GetBids(bidInfos))
	require.Error(t, err)

	repair, bidInfos, getter, _ = InitMockRepair(codemode.EC6P10L2)
	badi = []uint8{16}
	for idx := range [10]struct{}{} {
		getter.setFail(repair.locations[idx].Vuid, errors.New("fake error"))
	}
	err = repair.recoverReplicaShards(context.Background(), badi, GetBids(bidInfos))
	require.NoError(t, err)
	testCheckData(t, repair, getter, badi)

	repair, bidInfos, getter, _ = InitMockRepair(codemode.EC16P20L2)
	badi = []uint8{34}
	err = repair.recoverReplicaShards(context.Background(), badi, GetBids(bidInfos))
	require.NoError(t, err)
	testCheckData(t, repair, getter, badi)
}

func TestRecoverShards(t *testing.T) {
	ctx := context.Background()
	repair1, _, getter2, _ := InitMockRepair(codemode.EC15P12)
	badi1 := []uint8{0, 3, 6, 11, 16, 17} // local 16 17
	err := repair1.RecoverShards(ctx, badi1, false)
	require.NoError(t, err)
	testCheckData(t, repair1, getter2, badi1)

	// test EC6p6
	repair2, _, getter2, _ := InitMockRepair(codemode.EC6P6)
	badi2 := []uint8{0, 2, 4, 6, 8, 10}
	err = repair2.RecoverShards(ctx, badi2, false)
	require.NoError(t, err)
	testCheckData(t, repair2, getter2, badi2)

	// test EC15P19L2
	repair3, _, getter3, _ := InitMockRepair(codemode.EC16P20L2)
	badi3 := []uint8{0, 15, 34}
	err = repair3.RecoverShards(ctx, badi3, false)
	require.NoError(t, err)
	testCheckData(t, repair3, getter3, badi3)

	// test EC6P10L2
	repair4, _, getter4, _ := InitMockRepair(codemode.EC6P10L2)
	badi4 := []uint8{0, 6, 16}
	err = repair4.RecoverShards(ctx, badi4, false)
	require.NoError(t, err)
	testCheckData(t, repair4, getter4, badi4)

	comb := func(n, k int) int {
		lgmma1, _ := math.Lgamma(float64(n + 1))
		lgmma2, _ := math.Lgamma(float64(k + 1))
		lgmma3, _ := math.Lgamma(float64(n - k + 1))
		return int(math.Round(math.Exp(lgmma1 - lgmma2 - lgmma3)))
	}

	// test AzureLrc EC12P6L3
	repair5, _, getter5, _ := InitMockRepair(codemode.EC12P6L3)
	n := 12
	k := 7
	combinationCnt := comb(n, k)
	tmpCombination := make([]uint8, k)
	for i := range tmpCombination {
		tmpCombination[i] = uint8(i)
	}
	for i := 0; i < combinationCnt; i++ {
		badi5 := make([]uint8, len(tmpCombination))
		badi5 = append(badi5, tmpCombination...)
		log.Println(badi5)
		err = repair5.RecoverShards(ctx, badi5, false)
		require.NoError(t, err)
		testCheckData(t, repair5, getter5, badi5)
		if i < combinationCnt-1 {
			j := k - 1
			for j >= 0 && tmpCombination[j] == uint8(n-k+j) {
				j--
			}
			tmpCombination[j]++
			for j = j + 1; j < k; j++ {
				tmpCombination[j] = tmpCombination[j-1] + 1
			}
		}
	}

	// test AzureLrc EC12P3L3
	repair6, _, getter6, _ := InitMockRepair(codemode.EC12P3L3)
	badi6 := []uint8{0, 1, 2, 3}
	err = repair6.RecoverShards(ctx, badi6, false)
	require.NoError(t, err)
	testCheckData(t, repair6, getter6, badi6)

	// test AzureLrc EC18P9L3
	repair7, _, getter7, _ := InitMockRepair(codemode.EC18P9L3)
	badi7 := []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	err = repair7.RecoverShards(ctx, badi7, false)
	require.NoError(t, err)
	testCheckData(t, repair7, getter7, badi7)

	// test AzureLrc EC12P6L3
	repair8, _, getter8, _ := InitMockRepair(codemode.EC10P5L3)
	badi8 := []uint8{0, 1, 2, 3, 4, 5}
	err = repair8.RecoverShards(ctx, badi8, false)
	require.NoError(t, err)
	testCheckData(t, repair8, getter8, badi8)
}

func TestRecoverShards2(t *testing.T) {
	// test without local :eg EC6p6
	ctx := context.Background()

	tcCases := []struct {
		mode  codemode.CodeMode
		badis []uint8
		err   error
	}{
		{
			mode:  codemode.EC6P6,
			badis: []uint8{0, 1, 2, 3, 4, 5},
			err:   nil,
		},
		{
			mode:  codemode.EC6P6,
			badis: []uint8{6, 7, 8, 9, 10, 11},
			err:   nil,
		},
		{
			mode:  codemode.EC6P6,
			badis: []uint8{0, 1, 2, 6, 7, 8},
			err:   nil,
		},
		{
			mode:  codemode.EC6P6,
			badis: []uint8{1, 2, 3, 4, 6, 7, 8},
			err:   errBidCanNotRecover,
		},
		// test with local :egEC6P10L2
		// N broken
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{0, 1, 2, 3, 4, 5},
			err:   nil,
		},
		// M broken
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
			err:   nil,
		},
		// mix broken N,M
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			err:   nil,
		},
		// broken L
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{16, 17},
			err:   nil,
		},
		// broken mix N,M,L
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 16, 17},
			err:   nil,
		},
		// broken many
		{
			mode:  codemode.EC6P10L2,
			badis: []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			err:   errBidCanNotRecover,
		},
	}

	for _, tc := range tcCases {
		repair, _, getter, _ := InitMockRepair(tc.mode)
		err := repair.RecoverShards(ctx, tc.badis, false)
		if tc.err == nil {
			require.NoError(t, err)
			testCheckData(t, repair, getter, tc.badis)
			continue
		}
		require.EqualError(t, tc.err, err.Error())
	}
}

func TestLocalStripes(t *testing.T) {
	mode := codemode.EC6P10L2
	replicas := genMockVol(1, mode)
	repair := NewShardRecover(replicas, mode, nil, nil, 4, proto.TaskTypeShardRepair, false)
	for idx, replica := range replicas {
		require.Equal(t, idx, int(replica.Vuid.Index()))
	}
	badi := []uint8{0}
	localStripeIdxs := []uint8{}
	stripes, err := repair.genLocalStripes(badi)
	require.NoError(t, err)
	for _, stripe := range stripes {
		for _, replica := range stripe.locations {
			localStripeIdxs = append(localStripeIdxs, replica.Vuid.Index())
		}
		require.Equal(t, []uint8{0, 1, 2, 6, 7, 8, 9, 10, 16}, localStripeIdxs)
	}
}

func testCheckData(t *testing.T, repairer *ShardRecover, getter *MockGetter, badi []uint8) {
	for _, bidInfo := range repairer.repairBidsReadOnly {
		for _, repl := range repairer.locations {
			if repairer.chunksShardsBuf[repl.Vuid.Index()] == nil {
				continue
			}
			if repairer.chunksShardsBuf[repl.Vuid.Index()].shardIsOk(bidInfo.Bid) {
				data, err := repairer.chunksShardsBuf[repl.Vuid.Index()].FetchShard(bidInfo.Bid)
				require.NoError(t, err)
				crc1 := crc32.ChecksumIEEE(data)
				crc2 := getter.getShardCrc32(repl.Vuid, bidInfo.Bid)
				require.Equal(t, crc2, crc1)
			}
		}
		for _, repairIdx := range badi {
			if !repairer.chunksShardsBuf[repairIdx].shardIsOk(bidInfo.Bid) {
				require.NoError(t, errors.New("some bid has not been repaired"), repairIdx, bidInfo.Bid)
			}
		}
	}
}

func TestDownload(t *testing.T) {
	ctx := context.Background()
	repair, _, getter, replicas := InitMockRepair(codemode.EC6P6)
	repairBids := []proto.BlobID{1, 2, 4, 5, 6, 7}
	idxs := replicas.Indexes()
	err := repair.allocBuf(ctx, idxs)
	require.NoError(t, err)

	failVuids := []proto.Vuid{replicas[0].Vuid, replicas[1].Vuid}
	for _, fail := range failVuids {
		getter.setFail(fail, errors.New("fake error"))
	}
	repair.download(ctx, repairBids, replicas)

	for _, fail := range failVuids {
		for _, bid := range repairBids {
			_, err := repair.GetShard(fail.Index(), bid)
			require.Error(t, err)
			require.EqualError(t, errcode.ErrShardPartialRepairFailed, err.Error())
		}
	}

	var well []proto.Vuid
	for _, repl := range replicas {
		isFail := false
		for _, fail := range failVuids {
			if fail == repl.Vuid {
				isFail = true
				break
			}
		}
		if !isFail {
			well = append(well, repl.Vuid)
		}
	}

	for _, repl := range well {
		for _, bid := range repairBids {
			_, err := repair.GetShard(repl.Index(), bid)
			require.NoError(t, err)
		}
	}
}

func TestDirect(t *testing.T) {
	ctx := context.Background()
	repair, _, getter, _ := InitMockRepair(codemode.EC6P6)
	badi := []uint8{0, 1, 2, 3, 4, 5}
	err := repair.RecoverShards(ctx, badi, true)
	require.NoError(t, err)
	testCheckData(t, repair, getter, badi)
}

func TestMemEnough(t *testing.T) {
	// direct get
	ctx := context.Background()
	repair, _, _, _ := InitMockRepair(codemode.EC6P6)

	badi := []uint8{0, 1, 2, 3, 4, 5}
	err := repair.RecoverShards(ctx, badi, true)
	require.NoError(t, err)

	// local stripe
	repair1, repairBids, _, _ := InitMockRepair(codemode.EC6P3L3)
	badi1 := []uint8{11}
	err = repair1.recoverReplicaShards(ctx, badi1, []proto.BlobID{repairBids[0].Bid})
	require.NoError(t, err)

	repair1, repairBids, getter, replicas := InitMockRepair(codemode.EC6P3L3)
	badi1 = []uint8{11}
	getter.setFail(replicas[5].Vuid, errors.New("fake error"))
	getter.setFail(replicas[9].Vuid, errors.New("fake error"))
	err = repair1.recoverReplicaShards(ctx, badi1, []proto.BlobID{repairBids[0].Bid})
	require.NoError(t, err)

	// global stripe
	repair2, repairBids, _, _ := InitMockRepair(codemode.EC6P3L3)
	badi2 := []uint8{0}
	err = repair2.recoverReplicaShards(ctx, badi2, []proto.BlobID{repairBids[0].Bid})
	require.NoError(t, err)

	repair2, repairBids, getter, replicas2 := InitMockRepair(codemode.EC6P3L3)

	badi2 = []uint8{0}
	getter.setFail(replicas2[1].Vuid, errors.New("fake error"))
	getter.setFail(replicas2[9].Vuid, errors.New("fake error"))

	err = repair2.recoverReplicaShards(ctx, badi2, []proto.BlobID{repairBids[0].Bid})
	require.NoError(t, err)
}
