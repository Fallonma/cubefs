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

package metanode

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/cubefs/cubefs/proto"
	"github.com/stretchr/testify/require"
)

func newTestRocksTree(dir string) (rocksTree *RocksTree) {
	err := os.RemoveAll(dir)
	if err != nil {
		fmt.Printf("remove db dir(%s) failed\n", dir)
		os.Exit(1)
	}
	rocksdbHandle := NewRocksdb()
	if err = rocksdbHandle.OpenDb(dir, 0, 0, 0, 0, 0, 0); err != nil {
		fmt.Printf("open db without exist dir(%s) failed:%v\n", dir, err)
		os.Exit(1)
	}
	rocksTree, err = DefaultRocksTree(rocksdbHandle, 0)
	if err != nil {
		fmt.Printf("new rocks tree in dir(%s) failed:%v\n", dir, err)
		os.Exit(1)
	}
	return
}

func mockTree(rocksTree *RocksTree, treeType TreeType) (memModeTree, rocksModeTree interface{}) {
	switch treeType {
	case InodeType:
		memModeTree = &InodeBTree{NewBtree()}
		rocksModeTree, _ = NewInodeRocks(rocksTree)
	case DentryType:
		memModeTree = &DentryBTree{NewBtree()}
		rocksModeTree, _ = NewDentryRocks(rocksTree)
	case MultipartType:
		memModeTree = &MultipartBTree{NewBtree()}
		rocksModeTree, _ = NewMultipartRocks(rocksTree)
	case ExtendType:
		memModeTree = &ExtendBTree{NewBtree()}
		rocksModeTree, _ = NewExtendRocks(rocksTree)
	case TransactionType:
		memModeTree = &TransactionBTree{NewBtree()}
		rocksModeTree, _ = NewTransactionRocks(rocksTree)
	case TransactionRollbackInodeType:
		memModeTree = &TransactionBTree{NewBtree()}
		rocksModeTree, _ = NewTransactionRollbackInodeRocks(rocksTree)
	case TransactionRollbackDentryType:
		memModeTree = &TransactionBTree{NewBtree()}
		rocksModeTree, _ = NewTransactionRollbackDentryRocks(rocksTree)
	case DeletedExtentsType:
		memModeTree = &DeletedExtentsBTree{NewBtree()}
		rocksModeTree, _ = NewDeletedExtentsRocks(rocksTree)
	case DeletedObjExtentsType:
		memModeTree = &DeletedObjExtentsBTree{NewBtree()}
		rocksModeTree, _ = NewDeletedObjExtentsRocks(rocksTree)
	default:
		fmt.Printf("error tree type(%v)\n", treeType)
		os.Exit(1)
	}
	return
}

func InitInodeTree(rocksTree *RocksTree) (memInodeTree, rocksInodeTree InodeTree) {
	if rocksTree == nil {
		fmt.Printf("rocksTree is nil\n")
		os.Exit(1)
	}
	memItem, rocksItem := mockTree(rocksTree, InodeType)
	memInodeTree = memItem.(InodeTree)
	rocksInodeTree = rocksItem.(InodeTree)
	return
}

func InitDentryTree(rocksTree *RocksTree) (memDentryTree, rocksDentryTree DentryTree) {
	if rocksTree == nil {
		fmt.Printf("rocksTree is nil\n")
		os.Exit(1)
	}
	memItem, rocksItem := mockTree(rocksTree, DentryType)
	memDentryTree = memItem.(DentryTree)
	rocksDentryTree = rocksItem.(DentryTree)
	return
}

func inodeCreate(inodeTree InodeTree, inode *Inode, replace bool) (ino *Inode, ok bool, err error) {
	var dbWriteHandle interface{}
	dbWriteHandle, err = inodeTree.CreateBatchWriteHandle()
	if err != nil {
		return
	}
	defer inodeTree.ReleaseBatchWriteHandle(dbWriteHandle)

	ino, ok, err = inodeTree.Create(dbWriteHandle, inode, replace)
	if err != nil {
		return
	}

	if !ok {
		return
	}

	err = inodeTree.CommitBatchWrite(dbWriteHandle, false)
	return
}

func inodePut(inodeTree InodeTree, inode *Inode) (err error) {
	var dbWriteHandle interface{}
	dbWriteHandle, err = inodeTree.CreateBatchWriteHandle()
	if err != nil {
		return
	}
	defer inodeTree.ReleaseBatchWriteHandle(dbWriteHandle)

	err = inodeTree.Put(dbWriteHandle, inode)
	if err != nil {
		return
	}

	err = inodeTree.CommitBatchWrite(dbWriteHandle, false)
	return
}

func inodeDelete(inodeTree InodeTree, ino uint64) (ok bool, err error) {
	var dbWriteHandle interface{}
	dbWriteHandle, err = inodeTree.CreateBatchWriteHandle()
	if err != nil {
		return
	}
	defer inodeTree.ReleaseBatchWriteHandle(dbWriteHandle)

	ok, err = inodeTree.Delete(dbWriteHandle, ino)
	if err != nil {
		return
	}

	if !ok {
		return
	}

	err = inodeTree.CommitBatchWrite(dbWriteHandle, false)
	return
}

func dentryCreate(dentryTree DentryTree, dentry *Dentry, replace bool) (den *Dentry, ok bool, err error) {
	var dbWriteHandle interface{}
	dbWriteHandle, err = dentryTree.CreateBatchWriteHandle()
	if err != nil {
		return
	}
	defer dentryTree.ReleaseBatchWriteHandle(dbWriteHandle)

	den, ok, err = dentryTree.Create(dbWriteHandle, dentry, replace)
	if err != nil {
		return
	}

	if !ok {
		return
	}

	err = dentryTree.CommitBatchWrite(dbWriteHandle, false)
	return
}

func newInodeTree(t *testing.T, storeMode proto.StoreMode, rocksTree *RocksTree) InodeTree {
	switch storeMode {
	case proto.StoreModeMem:
		memItem, _ := mockTree(&RocksTree{}, InodeType)
		return memItem.(InodeTree)
	case proto.StoreModeRocksDb:
		if rocksTree == nil {
			t.Errorf("rocksTree is nil\n")
			t.FailNow()
		}
		_, rocksItem := mockTree(rocksTree, InodeType)
		return rocksItem.(InodeTree)
	default:
		t.Errorf("error store mode:%v", storeMode)
		t.FailNow()
		return nil
	}
}

func newDentryTree(t *testing.T, storeMode proto.StoreMode, rocksTree *RocksTree) DentryTree {
	switch storeMode {
	case proto.StoreModeMem:
		memItem, _ := mockTree(&RocksTree{}, DentryType)
		return memItem.(DentryTree)
	case proto.StoreModeRocksDb:
		if rocksTree == nil {
			t.Errorf("rocksTree is nil\n")
			t.FailNow()
		}
		_, rocksItem := mockTree(rocksTree, DentryType)
		return rocksItem.(DentryTree)
	default:
		t.Errorf("error store mode:%v", storeMode)
		t.FailNow()
		return nil
	}
}

func TestInodeTree_Create(t *testing.T) {
	tests := []struct {
		name       string
		storeMode  proto.StoreMode
		rocksDBDir string
		inode      *Inode
	}{
		{
			name:       "MemMode",
			storeMode:  proto.StoreModeMem,
			rocksDBDir: "",
			inode: &Inode{
				Inode:      1000,
				Type:       uint32(os.ModeDir),
				Uid:        0,
				Gid:        0,
				Size:       0,
				Generation: 0,
				LinkTarget: []byte("linkTarget"),
				NLink:      3,
				Extents:    NewSortedExtents(),
				ObjExtents: NewSortedObjExtents(),
			},
		},
		{
			name:       "RocksDBMode",
			storeMode:  proto.StoreModeRocksDb,
			rocksDBDir: "./test_inode_create",
			inode: &Inode{
				Inode:      20,
				Type:       470,
				Uid:        0,
				Gid:        0,
				Size:       4096,
				NLink:      1,
				Extents:    NewSortedExtents(),
				ObjExtents: NewSortedObjExtents(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var rocksTree *RocksTree
			if test.storeMode == proto.StoreModeRocksDb {
				rocksTree = newTestRocksTree(test.rocksDBDir)
				defer func() {
					rocksTree.db.CloseDb()
					_ = os.RemoveAll(test.rocksDBDir)
				}()
			}
			inodeTree := newInodeTree(t, test.storeMode, rocksTree)
			_, _, err := inodeCreate(inodeTree, test.inode, true)
			if err != nil {
				t.Errorf(err.Error())
				return
			}

			var existIno *Inode
			if existIno, err = inodeTree.Get(test.inode.Inode); err != nil {
				t.Errorf(err.Error())
				return
			}

			require.EqualValues(t, test.inode, existIno)

			var ok = false
			_, ok, err = inodeCreate(inodeTree, test.inode, false)
			if err != nil {
				t.Errorf(err.Error())
				return
			}

			if ok {
				t.Errorf("create exist inode result mismatch, expect:false, actual:true")
			}

			if test.storeMode == proto.StoreModeRocksDb {
				if err = inodeTree.(*InodeRocks).RocksTree.LoadBaseInfo(); err != nil {
					t.Errorf("load base info failed:%v", err)
				}
			}

			if realCount := inodeTree.RealCount(); realCount != 1 || inodeTree.Count() != 1 {
				t.Errorf("inode count mismatch, expect:1, actual:[real count:%v, count:%v]", realCount, inodeTree.Count())
			}

			ok = false
			if ok, err = inodeDelete(inodeTree, test.inode.Inode); err != nil {
				t.Errorf(err.Error())
				return
			}

			if !ok {
				t.Errorf("inode(%s) delete result mismatch, expect:exist, actual:not exist", test.inode)
				return
			}

			if existIno, err = inodeTree.Get(test.inode.Inode); err != nil {
				t.Errorf(err.Error())
				return
			}

			if existIno != nil {
				t.Errorf("inode(%s) has been deleted, but inode get not nil", test.inode)
				return
			}

			t.Logf("%s test success", test.name)
		})
	}
}

func TestInodeTreeCreate(t *testing.T) {
	//check
	var errForMem, errForRocks error

	rocksDir := "./test_inode_tree_create"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memInodeTree, rocksInodeTree := InitInodeTree(rocksTree)
	//create
	inode := NewInode(1000, 0)
	_, _, errForMem = inodeCreate(memInodeTree, inode, true)
	_, _, errForRocks = inodeCreate(rocksInodeTree, inode, true)
	if errForRocks != nil || errForMem != nil {
		t.Fatalf("Test Create: error different or mismatch, expect:nil, actual[errorInMem:%v errorInRocks:%v]", errForMem, errForRocks)
	}

	var ok = false
	_, ok, _ = inodeCreate(memInodeTree, inode, false)
	if ok {
		t.Fatalf("Test Create: create exist inode success, expcet:false, actual:true")
	}
	_, ok, errForRocks = inodeCreate(rocksInodeTree, inode, false)
	if errForRocks != nil {
		t.Fatalf("inode create failed:%v", errForRocks)
	}
	if ok {
		t.Fatalf("Test Create: create exist inode success, expcet:false, actual:true")
	}

	_, _, errForMem = inodeCreate(memInodeTree, inode, true)
	_, _, errForRocks = inodeCreate(rocksInodeTree, inode, true)
	if errForMem != errForRocks || errForMem != nil {
		t.Fatalf("Test Create: error different or mismatch, expect:nil, actual[errorInMem:%v errorInRocks:%v]", errForMem, errForRocks)
	}

	if memInodeTree.Count() != rocksInodeTree.Count() || memInodeTree.Count() != 1 {
		t.Fatalf("Test Create: inode count different or mismatch, expect:1, actual:[mem:%v, rocks:%v]", memInodeTree.Count(), rocksInodeTree.Count())
	}

	_, errForMem = inodeDelete(memInodeTree, inode.Inode)
	_, errForRocks = inodeDelete(rocksInodeTree, inode.Inode)
	if errForMem != nil || errForRocks != nil {
		t.Fatalf("Test Create: delete inode failed, error[mem:%v, rocks:%v]", errForMem, errForRocks)
	}
	if memInodeTree.Count() != rocksInodeTree.Count() || memInodeTree.Count() != 0 {
		t.Fatalf("Test Create: inode count different or mismatch, expect:0, actual:[mem:%v, rocks:%v]", memInodeTree.Count(), rocksInodeTree.Count())
	}
}

func TestInodeTreeGet(t *testing.T) {
	var (
		errForMem   error
		errForRocks error
	)

	rocksDir := "./test_inode_tree_get"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memInodeTree, rocksInodeTree := InitInodeTree(rocksTree)
	//create
	for index := 1; index <= 100; index++ {
		inode := NewInode(uint64(index), 0)
		_, _, errForMem = inodeCreate(memInodeTree, inode, true)
		_, _, errForRocks = inodeCreate(rocksInodeTree, inode, true)
	}
	defer func() {
		for index := 1; index <= 100; index++ {
			_, errForMem = inodeDelete(memInodeTree, uint64(index))
			_, errForRocks = inodeDelete(rocksInodeTree, uint64(index))
		}
	}()

	if memInodeTree.Count() != rocksInodeTree.Count() || memInodeTree.Count() != 100 {
		t.Fatalf("Test Get: inode count mismatch, expect:100, actual:%v", memInodeTree.Count())
	}

	if rocksInodeTree.RealCount() != memInodeTree.RealCount() || rocksInodeTree.RealCount() != 100 {
		t.Fatalf("Test Get: inode count mismatch, expect:100, actual:%v", rocksInodeTree.RealCount())
	}

	var getIno *Inode
	getIno, errForMem = memInodeTree.Get(30)
	if errForMem != nil || getIno == nil {
		t.Fatalf("Test Get: result mismatch, expect[err:nil, inode:not nil] actual[err:%v, inode:%v]", errForMem, getIno.Inode)
	}

	if getIno.Inode != 30 {
		t.Fatalf("Test Get: inode number mismatch, expect:30, actual:%v", getIno.Inode)
	}

	getIno, errForRocks = rocksInodeTree.Get(30)
	if errForRocks != nil || getIno == nil {
		t.Fatalf("Test Get: result mismatch, expect[err:nil, inode:30] actual[err:%v, inode:%v]", errForRocks, getIno.Inode)
	}

	if getIno.Inode != 30 {
		t.Fatalf("Test Get: inode number mismatch, expect:30, actual:%v", getIno.Inode)
	}

	//get not exist inode
	getIno, errForMem = memInodeTree.Get(1000)
	if errForMem != nil {
		t.Fatalf("Test Get: result mismatch, expect[err:nil] actual[err:%v]", errForMem)
	}

	if getIno != nil {
		t.Fatalf("Test Get: result mismatch, inode get expect:nil, actual:%v", getIno)
	}

	getIno, errForRocks = rocksInodeTree.Get(1000)
	if errForRocks != nil && errForRocks != ErrRocksdbOperation {
		t.Fatalf("Test Get: result mismatch, expect[err:nil] actual[err:%v]", errForRocks)
	}

	if getIno != nil {
		t.Fatalf("Test Get: result mismatch, inode get expect:nil, actual:%v", getIno)
	}
	return
}

func TestInodeTreeGetMaxInode(t *testing.T) {
	var (
		errForRocks error
		inodeCount  = 1000
	)
	rocksDir := "./test_inode_tree_getMaxInode"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memInodeTree, rocksInodeTree := InitInodeTree(rocksTree)
	//create
	for index := 1; index <= inodeCount; index++ {
		inode := NewInode(uint64(index), 0)
		_, _, _ = inodeCreate(memInodeTree, inode, true)
		_, _, _ = inodeCreate(rocksInodeTree, inode, true)
	}

	maxIno, _ := memInodeTree.GetMaxInode()
	if maxIno != uint64(inodeCount) {
		t.Fatalf("Test GetMaxInode: result mismatch, expect:10000, actual:%v", maxIno)
	}

	maxIno, errForRocks = rocksInodeTree.GetMaxInode()
	if errForRocks != nil {
		t.Fatalf("Test GetMaxInode: get error:%v", errForRocks)
	}
	if maxIno != uint64(inodeCount) {
		t.Fatalf("Test GetMaxInode: result mismatch, expect:10000, actual:%v", maxIno)
	}
	return
}

func TestInodeTreeRange(t *testing.T) {
	inodeCount := 123
	rocksDir := "./test_inode_tree_range"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memInodeTree, rocksInodeTree := InitInodeTree(rocksTree)
	//create
	for index := 1; index <= inodeCount; index++ {
		inode := NewInode(uint64(index), 0)
		_, _, _ = inodeCreate(memInodeTree, inode, true)
		_, _, _ = inodeCreate(rocksInodeTree, inode, true)
	}

	startInode := 43
	index := 0
	memInodeTree.Range(NewInode(43, 0), nil, func(inode *Inode) (bool, error) {
		if inode.Inode != uint64(startInode+index) {
			t.Fatalf("Test Range: inode mismatch, expect:%v, actual:%v", startInode+index, inode.Inode)
		}
		index++
		return true, nil
	})
	if index != 81 {
		t.Fatalf("Test Range: inode count mismatch, expect:81, actual:%v", index)
	}

	index = 0
	rocksInodeTree.Range(NewInode(43, 0), nil, func(inode *Inode) (bool, error) {
		if inode.Inode != uint64(startInode+index) {
			t.Fatalf("Test Range: inode mismatch, expect:%v, actual:%v", startInode+index, inode.Inode)
		}
		index++
		return true, nil
	})
	if index != 81 {
		t.Fatalf("Test Range: inode count mismatch, expect:81, actual:%v", index)
	}
}

func TestInodeTreeMaxItem(t *testing.T) {
	var (
		inodeCount = 1234
	)
	rocksDir := "./test_inode_tree_MaxItem"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memInodeTree, rocksInodeTree := InitInodeTree(rocksTree)
	//create
	for index := 1; index <= inodeCount; index++ {
		inode := NewInode(uint64(index), 0)
		_, _, _ = inodeCreate(memInodeTree, inode, true)
		_, _, _ = inodeCreate(rocksInodeTree, inode, true)
	}

	maxInode := memInodeTree.MaxItem()
	if maxInode == nil || maxInode.Inode != uint64(inodeCount) {
		t.Fatalf("Test GetMaxInode: mem mode result mismatch, expect MaxInodeId:%v, actual MaxInode:%v", inodeCount, maxInode)
	}

	maxInode = rocksInodeTree.MaxItem()
	if maxInode == nil || maxInode.Inode != uint64(inodeCount) {
		t.Fatalf("Test GetMaxInode: rocks mode result mismatch, expect MaxInodeId:%v, actual MaxInode:%v", inodeCount, maxInode)
	}
	return
}

func TestDentryTreeRange(t *testing.T) {
	rocksDir := "./test_dentry_tree_range"
	rocksTree := newTestRocksTree(rocksDir)
	defer func() {
		rocksTree.db.CloseDb()
		_ = os.RemoveAll(rocksDir)
	}()
	memDentryTree, rocksDentryTree := InitDentryTree(rocksTree)
	dentry := &Dentry{ParentId: 1, Name: "test_01", Inode: 1001, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 1, Name: "test_02", Inode: 1002, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 1, Name: "test_03", Inode: 1003, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 1, Name: "test_04", Inode: 1004, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 1, Name: "abc", Inode: 1005, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 1, Name: "def", Inode: 1006, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	dentry = &Dentry{ParentId: 2, Name: "hig", Inode: 1007, Type: 0}
	_, _, _ = dentryCreate(memDentryTree, dentry, true)
	_, _, _ = dentryCreate(rocksDentryTree, dentry, true)

	expectResult := []*Dentry{
		{ParentId: 1, Name: "abc", Inode: 1005, Type: 0},
		{ParentId: 1, Name: "def", Inode: 1006, Type: 0},
		{ParentId: 1, Name: "test_01", Inode: 1001, Type: 0},
		{ParentId: 1, Name: "test_02", Inode: 1002, Type: 0},
		{ParentId: 1, Name: "test_03", Inode: 1003, Type: 0},
		{ParentId: 1, Name: "test_04", Inode: 1004, Type: 0},
	}
	actualResult := make([]*Dentry, 0, 6)
	memDentryTree.Range(&Dentry{ParentId: 1}, &Dentry{ParentId: 2}, func(den *Dentry) (bool, error) {
		actualResult = append(actualResult, den)
		return true, nil
	})
	if len(expectResult) != len(actualResult) {
		t.Fatalf("Test Dentry Range in mem: result cnt mismatch, expect:%v, actual:%v", len(expectResult), len(actualResult))
	}
	for index, den := range expectResult {
		if !reflect.DeepEqual(den, actualResult[index]) {
			t.Fatalf("Test Dentry Range in mem: result mismatch, expect:%s, actual:%s", den.String(), actualResult[index].String())
		}
	}

	actualResult = make([]*Dentry, 0, 6)
	rocksDentryTree.Range(&Dentry{ParentId: 1}, &Dentry{ParentId: 2}, func(den *Dentry) (bool, error) {
		actualResult = append(actualResult, den)
		return true, nil
	})
	if len(expectResult) != len(actualResult) {
		t.Fatalf("Test Dentry Range in rocks: result cnt mismatch, expect:%v, actual:%v", len(expectResult), len(actualResult))
	}
	for index, den := range expectResult {
		if !reflect.DeepEqual(den, actualResult[index]) {
			t.Fatalf("Test Dentry Range in rocksdb: result mismatch, expect:%s, actual:%s", den.String(), actualResult[index].String())
		}
	}

	expectResult = []*Dentry{
		{ParentId: 1, Name: "test_01", Inode: 1001, Type: 0},
		{ParentId: 1, Name: "test_02", Inode: 1002, Type: 0},
		{ParentId: 1, Name: "test_03", Inode: 1003, Type: 0},
		{ParentId: 1, Name: "test_04", Inode: 1004, Type: 0},
	}
	actualResult = make([]*Dentry, 0, 4)
	memDentryTree.Range(&Dentry{ParentId: 1, Name: "test"}, &Dentry{ParentId: 2}, func(den *Dentry) (bool, error) {
		actualResult = append(actualResult, den)
		return true, nil
	})
	if len(expectResult) != len(actualResult) {
		t.Fatalf("Test Dentry Range in mem: result cnt mismatch, expect:%v, actual:%v", len(expectResult), len(actualResult))
	}
	for index, den := range expectResult {
		if !reflect.DeepEqual(den, actualResult[index]) {
			t.Fatalf("Test Dentry Range in mem: result mismatch, expect:%s, actual:%s", den.String(), actualResult[index].String())
		}
	}

	actualResult = make([]*Dentry, 0, 6)
	rocksDentryTree.Range(&Dentry{ParentId: 1, Name: "test"}, &Dentry{ParentId: 2}, func(den *Dentry) (bool, error) {
		actualResult = append(actualResult, den)
		return true, nil
	})
	if len(expectResult) != len(actualResult) {
		t.Fatalf("Test Dentry Range in rocks: result cnt mismatch, expect:%v, actual:%v", len(expectResult), len(actualResult))
	}
	for index, den := range expectResult {
		if !reflect.DeepEqual(den, actualResult[index]) {
			t.Fatalf("Test Dentry Range in rocksdb: result mismatch, expect:%s, actual:%s", den.String(), actualResult[index].String())
		}
	}
}
