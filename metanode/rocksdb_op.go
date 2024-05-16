// Copyright 2024 The CubeFS Authors.
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
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"strings"
	"sync"
	"sync/atomic"

	"github.com/cubefs/cubefs/util"
	"github.com/cubefs/cubefs/util/log"
	"github.com/tecbot/gorocksdb"
)

const (
	DefaultCacheSize           = 256 * util.MB
	DefaultWriteBuffSize       = 256 * util.MB
	DefaultWriteBuffNum        = 4
	DefaultMinWriteBuffToMerge = 4
	DefaultMaxSubCompaction    = 4
	DefaultRetryCount          = 3
	DefaultMaxLogFileSize      = 1 * util.MB
	DefaultLogFileRollTime     = 3 * 24 * time.Hour // NOTE: 3 day
	DefaultKeepLogFileNum      = 3
)

var (
	ErrRocksdbAccess             = errors.New("access rocksdb error")
	ErrRocksdbOperation          = errors.New("rocksdb operation error")
	ErrInvalidRocksdbWriteHandle = errors.New("invalid rocksdb write batch")
	ErrInvalidRocksdbTableType   = errors.New("invalid rocksdb table type")
	ErrInvalidRocksdbSnapshot    = errors.New("invalid rocksdb snapshot")
)

type TableType byte

const (
	BaseInfoTable TableType = iota
	DentryTable
	InodeTable
	ExtendTable
	MultipartTable
	TransactionTable
	TransactionRollbackInodeTable
	TransactionRollbackDentryTable
	DeletedExtentsTable
	DeletedObjExtentsTable
	MaxTable
)

const (
	SyncWalTable = 255
)

func getTableTypeKey(treeType TreeType) TableType {
	switch treeType {
	case InodeType:
		return InodeTable
	case DentryType:
		return DentryTable
	case MultipartType:
		return MultipartTable
	case ExtendType:
		return ExtendTable
	case TransactionType:
		return TransactionTable
	case TransactionRollbackInodeType:
		return TransactionRollbackInodeTable
	case TransactionRollbackDentryType:
		return TransactionRollbackDentryTable
	case DeletedExtentsType:
		return DeletedExtentsTable
	case DeletedObjExtentsType:
		return DeletedObjExtentsTable
	default:
	}
	panic(ErrInvalidRocksdbTableType)
}

const (
	dbInitSt uint32 = iota
	dbOpenningSt
	dbOpenedSt
	dbClosingSt
	dbClosedSt
)

func isRetryError(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "Try again") {
		return true
	}
	return false
}

type RocksdbOperator struct {
	dir   string
	db    *gorocksdb.DB
	mutex sync.RWMutex
	state uint32

	readOption  *gorocksdb.ReadOptions
	writeOption *gorocksdb.WriteOptions
	openOption  *gorocksdb.Options
	cache       *gorocksdb.Cache
	tableOption *gorocksdb.BlockBasedTableOptions
}

func NewRocksdb() (operator *RocksdbOperator) {
	operator = &RocksdbOperator{
		state: dbInitSt,
	}
	return
}

func (db *RocksdbOperator) CloseDb() (err error) {
	log.LogDebugf("close RocksDB, Path(%s), State(%v)", db.dir, atomic.LoadUint32(&db.state))

	if ok := atomic.CompareAndSwapUint32(&db.state, dbOpenedSt, dbClosingSt); !ok {
		if atomic.LoadUint32(&db.state) == dbClosedSt {
			//already closed
			return nil
		}
		return fmt.Errorf("db state error, cur: %v, to:%v", db.state, dbClosingSt)
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()
	defer atomic.CompareAndSwapUint32(&db.state, dbClosingSt, dbClosedSt)

	db.db.Close()
	db.readOption.Destroy()
	db.writeOption.Destroy()
	db.openOption.Destroy()
	db.cache.Destroy()
	db.tableOption.Destroy()

	db.db = nil
	db.readOption = nil
	db.writeOption = nil
	db.openOption = nil
	db.tableOption = nil
	db.cache = nil
	return
}

func (dbInfo *RocksdbOperator) newRocksdbOptions(
	writeBufferSize int,
	writeBufferNum int,
	minWriteBuffToMerge int,
	maxSubCompactions int,
	blockCacheSize uint64,
	maxLogFileSize int,
	logFileTimeToRoll time.Duration,
	keepLogFileNum int) (opts *gorocksdb.Options, cache *gorocksdb.Cache, tableOpts *gorocksdb.BlockBasedTableOptions) {
	opts = gorocksdb.NewDefaultOptions()

	// NOTE: check and set default options
	if writeBufferSize == 0 {
		writeBufferSize = DefaultWriteBuffSize
	}
	if writeBufferNum == 0 {
		writeBufferNum = DefaultWriteBuffNum
	}
	if minWriteBuffToMerge == 0 {
		minWriteBuffToMerge = DefaultMinWriteBuffToMerge
	}
	if maxSubCompactions == 0 {
		maxSubCompactions = DefaultMaxSubCompaction
	}
	if maxLogFileSize == 0 {
		maxLogFileSize = DefaultMaxLogFileSize
	}
	if logFileTimeToRoll == 0 {
		logFileTimeToRoll = DefaultLogFileRollTime
	}
	if keepLogFileNum == 0 {
		keepLogFileNum = DefaultKeepLogFileNum
	}
	if blockCacheSize == 0 {
		blockCacheSize = DefaultCacheSize
	}

	// NOTE: main options
	opts.SetCreateIfMissing(true)
	opts.SetWriteBufferSize(writeBufferSize)
	opts.SetMaxWriteBufferNumber(writeBufferNum)
	opts.SetCompression(gorocksdb.NoCompression)
	opts.SetMinWriteBufferNumberToMerge(minWriteBuffToMerge)
	opts.SetLevelCompactionDynamicLevelBytes(true)
	tableOpts = gorocksdb.NewDefaultBlockBasedTableOptions()
	cache = gorocksdb.NewLRUCache(blockCacheSize)
	tableOpts.SetBlockCache(cache)
	opts.SetMaxSubCompactions(maxSubCompactions)
	opts.SetBlockBasedTableFactory(tableOpts)

	// NOTE: rocksdb log file options
	opts.SetMaxLogFileSize(maxLogFileSize)
	opts.SetLogFileTimeToRoll(int(logFileTimeToRoll.Seconds()))
	opts.SetKeepLogFileNum(keepLogFileNum)
	return
}

func (dbInfo *RocksdbOperator) doOpen(dir string, writeBufferSize int, writeBufferNum int, minWriteBuffToMerge int, maxSubCompactions int, blockCacheSize uint64, maxLogFileSize int, logFileTimeToRoll time.Duration, keepLogFileNum int) (err error) {
	var stat fs.FileInfo

	stat, err = os.Stat(dir)
	if err == nil && !stat.IsDir() {
		log.LogErrorf("interOpenDb path:[%s] is not dir", dir)
		return fmt.Errorf("path:[%s] is not dir", dir)
	}

	if err != nil && !os.IsNotExist(err) {
		log.LogErrorf("interOpenDb stat error: dir: %v, err: %v", dir, err)
		return err
	}

	// NOTE: mkdir all  will return nil when path exist and path is dir
	if err = os.MkdirAll(dir, os.ModePerm); err != nil {
		log.LogErrorf("interOpenDb mkdir error: dir: %v, err: %v", dir, err)
		return err
	}

	log.LogInfof("[doOpen] rocksdb dir(%v)", dir)
	dbInfo.openOption, dbInfo.cache, dbInfo.tableOption = dbInfo.newRocksdbOptions(writeBufferSize, writeBufferNum, minWriteBuffToMerge, maxSubCompactions, blockCacheSize, maxLogFileSize, logFileTimeToRoll, keepLogFileNum)

	dbInfo.db, err = gorocksdb.OpenDb(dbInfo.openOption, dir)

	if err != nil {
		log.LogErrorf("interOpenDb open db err:%v", err)
		return ErrRocksdbOperation
	}
	dbInfo.dir = dir
	dbInfo.readOption = gorocksdb.NewDefaultReadOptions()
	dbInfo.writeOption = gorocksdb.NewDefaultWriteOptions()
	// NOTE: we use raft wal, enable rocksdb wal is unnecessary
	dbInfo.writeOption.DisableWAL(true)
	return nil
}

func (dbInfo *RocksdbOperator) OpenDb(dir string, writeBufferSize int, writeBufferNum int, minWriteBuffToMerge int, maxSubCompactions int, blockCacheSize uint64, maxLogFileSize int, logFileTimeToRoll time.Duration, keepLogFileNum int) (err error) {
	ok := atomic.CompareAndSwapUint32(&dbInfo.state, dbInitSt, dbOpenningSt)
	ok = ok || atomic.CompareAndSwapUint32(&dbInfo.state, dbClosedSt, dbOpenningSt)
	if !ok {
		if atomic.LoadUint32(&dbInfo.state) == dbOpenedSt {
			//already opened
			return nil
		}
		return fmt.Errorf("db state error, cur: %v, to:%v", dbInfo.state, dbOpenningSt)
	}

	dbInfo.mutex.Lock()
	defer func() {
		if err == nil {
			atomic.CompareAndSwapUint32(&dbInfo.state, dbOpenningSt, dbOpenedSt)
		} else {
			log.LogErrorf("OpenDb failed, dir:%s error:%v", dir, err)
			atomic.CompareAndSwapUint32(&dbInfo.state, dbOpenningSt, dbInitSt)
		}
		dbInfo.mutex.Unlock()
	}()

	return dbInfo.doOpen(dir, writeBufferSize, writeBufferNum, minWriteBuffToMerge, maxSubCompactions, blockCacheSize, maxLogFileSize, logFileTimeToRoll, keepLogFileNum)
}

func (dbInfo *RocksdbOperator) ReOpenDb(dir string, writeBufferSize int, writeBufferNum int, minWriteBuffToMerge int, maxSubCompactions int, blockCacheSize uint64, maxLogFileSize int, logFileTimeToRoll time.Duration, keepLogFileNum int) (err error) {
	if ok := atomic.CompareAndSwapUint32(&dbInfo.state, dbClosedSt, dbOpenningSt); !ok {
		if atomic.LoadUint32(&dbInfo.state) == dbOpenedSt {
			//already opened
			return nil
		}
		return fmt.Errorf("db state error, cur: %v, to:%v", dbInfo.state, dbOpenningSt)
	}

	dbInfo.mutex.Lock()
	defer func() {
		if err == nil {
			atomic.CompareAndSwapUint32(&dbInfo.state, dbOpenningSt, dbOpenedSt)
		} else {
			atomic.CompareAndSwapUint32(&dbInfo.state, dbOpenningSt, dbClosedSt)
		}
		dbInfo.mutex.Unlock()
	}()

	if dbInfo == nil || (dbInfo.dir != "" && dbInfo.dir != dir) {
		return fmt.Errorf("rocks db dir changed, need new db instance")
	}

	return dbInfo.doOpen(dir, writeBufferSize, writeBufferNum, minWriteBuffToMerge, maxSubCompactions, blockCacheSize, maxLogFileSize, logFileTimeToRoll, keepLogFileNum)
}

func genRocksDBReadOption(snap *gorocksdb.Snapshot) (ro *gorocksdb.ReadOptions) {
	ro = gorocksdb.NewDefaultReadOptions()
	ro.SetFillCache(false)
	ro.SetSnapshot(snap)
	return
}

func (dbInfo *RocksdbOperator) iterator(ro *gorocksdb.ReadOptions) *gorocksdb.Iterator {
	return dbInfo.db.NewIterator(ro)
}

func (dbInfo *RocksdbOperator) rangeWithIter(it *gorocksdb.Iterator, start []byte, end []byte, cb func(k, v []byte) (bool, error)) error {
	it.Seek(start)
	for ; it.ValidForPrefix(start); it.Next() {
		key := it.Key().Data()
		value := it.Value().Data()
		if bytes.Compare(end, key) < 0 {
			break
		}
		if hasNext, err := cb(key, value); err != nil {
			log.LogErrorf("[RocksDB Op] RangeWithIter key: %v value: %v err: %v", key, value, err)
			return err
		} else if !hasNext {
			return nil
		}
	}
	return nil
}

func (dbInfo *RocksdbOperator) rangeWithIterByPrefix(it *gorocksdb.Iterator, prefix, start, end []byte, cb func(k, v []byte) (bool, error)) error {
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		key := it.Key().Data()
		value := it.Value().Data()
		if bytes.Compare(key, start) < 0 {
			continue
		}
		if bytes.Compare(end, key) < 0 {
			break
		}
		if hasNext, err := cb(key, value); err != nil {
			log.LogErrorf("[RocksTree] RangeWithIter key: %v value: %v err: %v", key, value, err)
			return err
		} else if !hasNext {
			return nil
		}
	}
	return nil
}

func (dbInfo *RocksdbOperator) descRangeWithIter(it *gorocksdb.Iterator, start []byte, end []byte, cb func(k, v []byte) (bool, error)) error {
	it.SeekForPrev(end)
	for ; it.ValidForPrefix(start); it.Prev() {
		key := it.Key().Data()
		value := it.Value().Data()
		if bytes.Compare(key, start) < 0 {
			break
		}
		if hasNext, err := cb(key, value); err != nil {
			log.LogErrorf("[RocksDB Op] RangeWithIter key: %v value: %v err: %v", key, value, err)
			return err
		} else if !hasNext {
			return nil
		}
	}
	return nil
}

func (dbInfo *RocksdbOperator) accessDb() error {
	if atomic.LoadUint32(&dbInfo.state) != dbOpenedSt {
		log.LogErrorf("[RocksDB Op] can not access db, db is not opened. Cur state:%v", dbInfo.state)
		return ErrRocksdbAccess
	}

	dbInfo.mutex.RLock()
	if atomic.LoadUint32(&dbInfo.state) != dbOpenedSt {
		dbInfo.mutex.RUnlock()
		log.LogErrorf("[RocksDB Op] can not access db, db is not opened. Cur state:%v", dbInfo.state)
		return ErrRocksdbAccess
	}
	return nil
}

func (dbInfo *RocksdbOperator) releaseDb() {
	dbInfo.mutex.RUnlock()
}

// NOTE: hold the lock while using snapshot
func (dbInfo *RocksdbOperator) OpenSnap() *gorocksdb.Snapshot {
	if err := dbInfo.accessDb(); err != nil {
		log.LogErrorf("[RocksDB Op] OpenSnap failed:%v", err)
		return nil
	}

	snap := dbInfo.db.NewSnapshot()
	if snap == nil {
		dbInfo.releaseDb()
	}
	return snap
}

func (dbInfo *RocksdbOperator) ReleaseSnap(snap *gorocksdb.Snapshot) {
	if snap == nil {
		return
	}
	defer dbInfo.releaseDb()

	dbInfo.db.ReleaseSnapshot(snap)
}

func (dbInfo *RocksdbOperator) RangeWithSnap(start, end []byte, snap *gorocksdb.Snapshot, cb func(k, v []byte) (bool, error)) (err error) {
	if snap == nil {
		return ErrInvalidRocksdbSnapshot
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	ro := genRocksDBReadOption(snap)
	it := dbInfo.iterator(ro)
	defer func() {
		it.Close()
		ro.Destroy()
	}()
	return dbInfo.rangeWithIter(it, start, end, cb)
}

func (dbInfo *RocksdbOperator) GetBytesWithSnap(snap *gorocksdb.Snapshot, key []byte) (value []byte, err error) {
	if snap == nil {
		err = ErrInvalidRocksdbSnapshot
		return
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	ro := genRocksDBReadOption(snap)
	for index := 0; index < DefaultRetryCount; {
		value, err = dbInfo.db.GetBytes(ro, key)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] GetBytes failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] GetBytes failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] GetBytes err:%v", err)
		err = ErrRocksdbOperation
		return
	}
	return
}

func (dbInfo *RocksdbOperator) RangeWithSnapByPrefix(prefix, start, end []byte, snap *gorocksdb.Snapshot, cb func(k, v []byte) (bool, error)) (err error) {
	if snap == nil {
		return ErrInvalidRocksdbSnapshot
	}

	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	ro := genRocksDBReadOption(snap)
	it := dbInfo.iterator(ro)
	defer func() {
		it.Close()
		ro.Destroy()
	}()
	return dbInfo.rangeWithIterByPrefix(it, prefix, start, end, cb)
}

func (dbInfo *RocksdbOperator) DescRangeWithSnap(start, end []byte, snap *gorocksdb.Snapshot, cb func(k, v []byte) (bool, error)) (err error) {
	if snap == nil {
		return ErrInvalidRocksdbSnapshot
	}

	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	ro := genRocksDBReadOption(snap)
	it := dbInfo.iterator(ro)
	defer func() {
		it.Close()
		ro.Destroy()
	}()
	return dbInfo.descRangeWithIter(it, start, end, cb)
}

func (dbInfo *RocksdbOperator) Range(start, end []byte, cb func(k, v []byte) (bool, error)) (err error) {
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	snapshot := dbInfo.db.NewSnapshot()
	ro := genRocksDBReadOption(snapshot)
	it := dbInfo.iterator(ro)
	defer func() {
		it.Close()
		ro.Destroy()
		dbInfo.db.ReleaseSnapshot(snapshot)
	}()
	return dbInfo.rangeWithIter(it, start, end, cb)
}

func (dbInfo *RocksdbOperator) DescRange(start, end []byte, cb func(k, v []byte) (bool, error)) (err error) {
	if err = dbInfo.accessDb(); err != nil {
		return err
	}
	defer dbInfo.releaseDb()

	snapshot := dbInfo.db.NewSnapshot()
	ro := genRocksDBReadOption(snapshot)
	it := dbInfo.iterator(ro)
	defer func() {
		it.Close()
		ro.Destroy()
		dbInfo.db.ReleaseSnapshot(snapshot)
	}()
	return dbInfo.descRangeWithIter(it, start, end, cb)
}

func (dbInfo *RocksdbOperator) GetBytes(key []byte) (bytes []byte, err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] GetBytes failed, error:%v", err)
		}
	}()

	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	for index := 0; index < DefaultRetryCount; {
		bytes, err = dbInfo.db.GetBytes(dbInfo.readOption, key)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] GetBytes failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] GetBytes failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] GetBytes err:%v", err)
		err = ErrRocksdbOperation
		return
	}
	return
}

func (dbInfo *RocksdbOperator) HasKey(key []byte) (bool, error) {
	bs, err := dbInfo.GetBytes(key)
	if err != nil {
		return false, err
	}
	return len(bs) > 0, nil
}

func (dbInfo *RocksdbOperator) Put(key, value []byte) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] Put failed, error:%v", err)
		}
	}()

	if err = dbInfo.accessDb(); err != nil {
		return err
	}
	defer dbInfo.releaseDb()
	for index := 0; index < DefaultRetryCount; {
		err = dbInfo.db.Put(dbInfo.writeOption, key, value)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] Put failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] Put failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] Put err:%v", err)
		err = ErrRocksdbOperation
		return
	}
	return
}

func (dbInfo *RocksdbOperator) Del(key []byte) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] Del failed, error:%v", err)
		}
	}()

	if err = dbInfo.accessDb(); err != nil {
		return err
	}
	defer dbInfo.releaseDb()
	for index := 0; index < DefaultRetryCount; {
		err = dbInfo.db.Delete(dbInfo.writeOption, key)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] Del failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] Del failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] Del err:%v", err)
		err = ErrRocksdbOperation
		return
	}
	return
}

func (dbInfo *RocksdbOperator) CreateBatchHandler() (interface{}, error) {
	var err error
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] CreateBatchHandler failed, error:%v", err)
		}
	}()

	if err = dbInfo.accessDb(); err != nil {
		return nil, err
	}
	defer dbInfo.releaseDb()
	batch := gorocksdb.NewWriteBatch()
	return batch, nil
}

func (dbInfo *RocksdbOperator) AddItemToBatch(handle interface{}, key, value []byte) (err error) {
	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		return ErrInvalidRocksdbWriteHandle
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	batch.Put(key, value)
	return nil
}

func (dbInfo *RocksdbOperator) DelItemToBatch(handle interface{}, key []byte) (err error) {
	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		return ErrInvalidRocksdbWriteHandle
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	batch.Delete(key)
	return nil
}

func (dbInfo *RocksdbOperator) DelRangeToBatch(handle interface{}, start []byte, end []byte) (err error) {
	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		return ErrInvalidRocksdbWriteHandle
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	batch.DeleteRange(start, end)
	return nil
}

func (dbInfo *RocksdbOperator) CommitBatchAndRelease(handle interface{}) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] CommitBatchAndRelease failed, err:%v", err)
		}
	}()

	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		err = ErrInvalidRocksdbWriteHandle
		return
	}

	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	for index := 0; index < DefaultRetryCount; {
		err = dbInfo.db.Write(dbInfo.writeOption, batch)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] CommitBatchAndRelease write failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] CommitBatchAndRelease write failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] CommitBatchAndRelease write failed:%v", err)
		err = ErrRocksdbOperation
		return
	}
	batch.Destroy()
	return
}

func (dbInfo *RocksdbOperator) HandleBatchCount(handle interface{}) (count int, err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] CommitBatchAndRelease failed, err:%v", err)
		}
	}()

	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		err = ErrInvalidRocksdbWriteHandle
		return
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	count = batch.Count()
	return
}

func (dbInfo *RocksdbOperator) CommitBatch(handle interface{}) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] CommitBatch failed, err:%v", err)
		}
	}()

	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		err = ErrInvalidRocksdbWriteHandle
		return
	}

	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	for index := 0; index < DefaultRetryCount; {
		err = dbInfo.db.Write(dbInfo.writeOption, batch)
		if err == nil {
			break
		}
		if !isRetryError(err) {
			log.LogErrorf("[RocksDB Op] CommitBatch write failed, error(%v)", err)
			break
		}
		log.LogErrorf("[RocksDB Op] CommitBatch write failed with retry error(%v), continue", err)
		index++
	}
	if err != nil {
		log.LogErrorf("[RocksDB Op] CommitBatch write failed, error(%v)", err)
		err = ErrRocksdbOperation
		return
	}
	return
}

func (dbInfo *RocksdbOperator) ReleaseBatchHandle(handle interface{}) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] ReleaseBatchHandle failed, err:%v", err)
		}
	}()

	if handle == nil {
		return
	}

	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		err = ErrInvalidRocksdbWriteHandle
		return
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	batch.Destroy()
	return
}

func (dbInfo *RocksdbOperator) ClearBatchWriteHandle(handle interface{}) (err error) {
	defer func() {
		if err != nil {
			log.LogErrorf("[RocksDB Op] ClearBatchWriteHandle failed, err:%v", err)
		}
	}()

	batch, ok := handle.(*gorocksdb.WriteBatch)
	if !ok {
		err = ErrInvalidRocksdbWriteHandle
		return
	}
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()
	batch.Clear()
	return
}

func (dbInfo *RocksdbOperator) CompactRange(start, end []byte) (err error) {
	if err = dbInfo.accessDb(); err != nil {
		return
	}
	defer dbInfo.releaseDb()

	dbInfo.db.CompactRange(gorocksdb.Range{
		Start: start,
		Limit: end,
	})
	return
}
