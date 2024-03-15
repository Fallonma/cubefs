// Copyright 2023 The CubeFS Authors.
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

package lcnode

import (
	"context"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/sdk/meta"
	"github.com/cubefs/cubefs/util/routinepool"
	"github.com/cubefs/cubefs/util/unboundedchan"
)

const (
	SnapScanTypeOnlyFile        int = 1
	SnapScanTypeOnlyDirAndDepth int = 2
)

type SnapshotScanner struct {
	ID          string
	Volume      string
	mw          MetaWrapper
	lcnode      *LcNode
	adminTask   *proto.AdminTask
	verDelReq   *proto.SnapshotVerDelTaskRequest
	inodeChan   *unboundedchan.UnboundedChan
	rPoll       *routinepool.RoutinePool
	currentStat *proto.SnapshotStatistics
	scanType    int
	stopC       chan bool
	ctx         context.Context
}

func NewSnapshotScanner(ctx context.Context, adminTask *proto.AdminTask, l *LcNode) (*SnapshotScanner, error) {
	request := adminTask.Request.(*proto.SnapshotVerDelTaskRequest)
	var err error
	metaConfig := &meta.MetaConfig{
		Volume:        request.Task.VolName,
		Masters:       l.masters,
		Authenticate:  false,
		ValidateOwner: false,
	}

	var metaWrapper *meta.MetaWrapper
	if metaWrapper, err = meta.NewMetaWrapper(metaConfig); err != nil {
		return nil, err
	}

	scanner := &SnapshotScanner{
		ID:          request.Task.Id,
		Volume:      request.Task.VolName,
		mw:          metaWrapper,
		lcnode:      l,
		adminTask:   adminTask,
		verDelReq:   request,
		inodeChan:   unboundedchan.NewUnboundedChan(defaultUnboundedChanInitCapacity),
		rPoll:       routinepool.NewRoutinePool(snapshotRoutineNumPerTask),
		currentStat: &proto.SnapshotStatistics{},
		stopC:       make(chan bool),
		ctx:         ctx,
	}
	return scanner, nil
}

func (l *LcNode) startSnapshotScan(ctx context.Context, adminTask *proto.AdminTask) (err error) {
	// new span and context with trace id
	span, ctx := proto.StartSpanFromContextWithTraceID(context.Background(), "", getSpan(ctx).TraceID())

	request := adminTask.Request.(*proto.SnapshotVerDelTaskRequest)
	span.Infof("startSnapshotScan: scan task(%v) received!", request.Task)
	response := &proto.SnapshotVerDelTaskResponse{}
	adminTask.Response = response

	l.scannerMutex.Lock()
	if _, ok := l.snapshotScanners[request.Task.Id]; ok {
		span.Infof("startSnapshotScan: scan task(%v) is already running!", request.Task)
		l.scannerMutex.Unlock()
		return
	}

	var scanner *SnapshotScanner
	scanner, err = NewSnapshotScanner(ctx, adminTask, l)
	if err != nil {
		span.Errorf("startSnapshotScan: NewSnapshotScanner err(%v)", err)
		response.Status = proto.TaskFailed
		response.Result = err.Error()
		l.scannerMutex.Unlock()
		return
	}
	l.snapshotScanners[scanner.ID] = scanner
	l.scannerMutex.Unlock()

	go scanner.Start()
	return
}

func (s *SnapshotScanner) getTaskVerSeq() uint64 {
	return s.verDelReq.Task.VolVersionInfo.Ver
}

func (s *SnapshotScanner) Stop() {
	span := getSpan(s.ctx)
	defer func() {
		if r := recover(); r != nil {
			span.Errorf("SnapshotScanner Stop err:%v", r)
		}
	}()
	close(s.stopC)
	s.rPoll.WaitAndClose()
	close(s.inodeChan.In)
	s.mw.Close()
	span.Debugf("snapshot scanner(%v) stopped", s.ID)
}

func (s *SnapshotScanner) Start() {
	response := s.adminTask.Response.(*proto.SnapshotVerDelTaskResponse)
	t := time.Now()
	response.StartTime = &t

	span := getSpan(s.ctx)
	// 1. delete all files
	span.Infof("snapshot startScan(%v): first round files start!", s.ID)
	s.scanType = SnapScanTypeOnlyFile
	go s.scan()
	firstDentry := &proto.ScanDentry{
		Inode: proto.RootIno,
		Type:  proto.Mode(os.ModeDir),
	}
	s.firstIn(firstDentry)
	s.checkScanning(false)

	// 2. delete all dirs
	span.Infof("snapshot startScan(%v): second round dirs start!", s.ID)
	s.scanType = SnapScanTypeOnlyDirAndDepth
	s.firstIn(firstDentry)
	s.checkScanning(true)
}

func (s *SnapshotScanner) firstIn(d *proto.ScanDentry) {
	span := getSpan(s.ctx)
	select {
	case <-s.stopC:
		span.Debugf("snapshot firstIn(%v): stopC!", s.ID)
		return
	default:
		s.inodeChan.In <- d
		span.Debugf("snapshot startScan(%v): scan type(%v), first dir dentry(%v) in!", s.ID, s.scanType, d)
	}
}

func (s *SnapshotScanner) getDirJob(dentry *proto.ScanDentry) (job func()) {
	span := getSpan(s.ctx).WithOperation("getDirJob")
	switch s.scanType {
	case SnapScanTypeOnlyDirAndDepth:
		span.Debug("SnapScanTypeOnlyDirAndDepth")
		job = func() {
			s.handleVerDelDepthFirst(dentry)
		}
	case SnapScanTypeOnlyFile:
		if s.inodeChan.Len() > maxDirChanNum {
			span.Debug("SnapScanTypeOnlyFile DepthFirst")
			job = func() {
				s.handleVerDelDepthFirst(dentry)
			}
		} else {
			span.Debug("SnapScanTypeOnlyFile BreadthFirst")
			job = func() {
				s.handleVerDelBreadthFirst(dentry)
			}
		}
	default:
		span.Errorf("invalid scanType: %v", s.scanType)
	}
	return
}

func (s *SnapshotScanner) scan() {
	span := getSpan(s.ctx)
	span.Debug("SnapshotScanner Enter scan")
	defer func() {
		span.Debug("SnapshotScanner Exit scan")
	}()
	for {
		select {
		case <-s.stopC:
			return
		case val, ok := <-s.inodeChan.Out:
			if !ok {
				span.Warn("inodeChan closed")
			} else {
				dentry := val.(*proto.ScanDentry)
				job := s.getDirJob(dentry)
				_, err := s.rPoll.Submit(job)
				if err != nil {
					span.Errorf("handlVerDel failed, err(%v)", err)
				}
			}
		}
	}
}

func (s *SnapshotScanner) handleVerDelDepthFirst(dentry *proto.ScanDentry) {
	span := getSpan(s.ctx).WithOperation("handleVerDelDepthFirst")
	var (
		children []proto.Dentry
		ino      *proto.InodeInfo
		err      error
	)
	onlyDir := s.scanType == SnapScanTypeOnlyDirAndDepth

	if os.FileMode(dentry.Type).IsDir() {
		marker := ""
		done := false

		for !done {
			children, err = s.mw.ReadDirLimitForSnapShotClean(s.ctx, dentry.Inode, marker, uint64(defaultReadDirLimit), s.getTaskVerSeq(), onlyDir)
			if err != nil && err != syscall.ENOENT {
				span.Errorf("ReadDirLimitForSnapShotClean failed, parent[%v] maker[%v] verSeq[%v] err[%v]",
					dentry.Inode, marker, s.getTaskVerSeq(), err)
				atomic.AddInt64(&s.currentStat.ErrorSkippedNum, 1)
				return
			}
			span.Debugf("ReadDirLimitForSnapShotClean parent[%v] maker[%v] verSeq[%v] children[%v]",
				dentry.Inode, marker, s.getTaskVerSeq(), len(children))

			if err == syscall.ENOENT {
				span.Errorf("ReadDirLimitForSnapShotClean failed, parent[%v] maker[%v] verSeq[%v] err[%v]",
					dentry.Inode, marker, s.getTaskVerSeq(), err)
				break
			}

			if marker != "" {
				if len(children) >= 1 && marker == children[0].Name {
					if len(children) <= 1 {
						span.Debugf("ReadDirLimit_ll done, parent[%v] maker[%v] verSeq[%v] children[%v]",
							dentry.Inode, marker, s.getTaskVerSeq(), children)
						break
					} else {
						skippedChild := children[0]
						children = children[1:]
						span.Debugf("ReadDirLimit_ll skip last marker[%v], parent[%v] verSeq[%v] skippedName[%v]",
							marker, dentry.Inode, s.getTaskVerSeq(), skippedChild.Name)
					}
				}
			}

			files := make([]*proto.ScanDentry, 0)
			dirs := make([]*proto.ScanDentry, 0)

			for _, child := range children {
				childDentry := &proto.ScanDentry{
					ParentId: dentry.Inode,
					Name:     child.Name,
					Inode:    child.Inode,
					Type:     child.Type,
				}
				if os.FileMode(childDentry.Type).IsDir() {
					dirs = append(dirs, childDentry)
				} else {
					files = append(files, childDentry)
				}
			}

			for _, file := range files {
				if ino, err = s.mw.Delete_Ver_ll(s.ctx, file.ParentId, file.Name, false, s.getTaskVerSeq(), file.Path); err != nil {
					span.Errorf("Delete_Ver_ll failed, file(parent[%v] child name[%v]) verSeq[%v] err[%v]",
						file.ParentId, file.Name, s.getTaskVerSeq(), err)
					atomic.AddInt64(&s.currentStat.ErrorSkippedNum, 1)
					return
				} else {
					span.Debugf("Delete_Ver_ll success, file(parent[%v] child name[%v]) verSeq[%v] ino[%v]",
						file.ParentId, file.Name, s.getTaskVerSeq(), ino)
					atomic.AddInt64(&s.currentStat.FileNum, 1)
					atomic.AddInt64(&s.currentStat.TotalInodeNum, 1)
				}
			}

			for _, dir := range dirs {
				s.handleVerDelDepthFirst(dir)
			}

			childrenNr := len(children)
			if (marker == "" && childrenNr < defaultReadDirLimit) || (marker != "" && childrenNr+1 < defaultReadDirLimit) {
				span.Debugf("ReadDirLimit_ll done, parent[%v]", dentry.Inode)
				done = true
			} else {
				marker = children[childrenNr-1].Name
				span.Debugf("ReadDirLimit_ll next marker[%v] parent[%v]", marker, dentry.Inode)
			}
		}
	}

	if onlyDir {
		if ino, err = s.mw.Delete_Ver_ll(s.ctx, dentry.ParentId, dentry.Name, os.FileMode(dentry.Type).IsDir(), s.getTaskVerSeq(), dentry.Path); err != nil {
			if dentry.ParentId >= 1 {
				span.Errorf("Delete_Ver_ll failed, dir(parent[%v] child name[%v]) verSeq[%v] err[%v]",
					dentry.ParentId, dentry.Name, s.getTaskVerSeq(), err)
				atomic.AddInt64(&s.currentStat.ErrorSkippedNum, 1)
				return
			}
		} else {
			span.Debugf("Delete_Ver_ll success, dir(parent[%v] child name[%v]) verSeq[%v] ino[%v]",
				dentry.ParentId, dentry.Name, s.getTaskVerSeq(), ino)
			atomic.AddInt64(&s.currentStat.DirNum, 1)
			atomic.AddInt64(&s.currentStat.TotalInodeNum, 1)
		}
	}
}

func (s *SnapshotScanner) handleVerDelBreadthFirst(dentry *proto.ScanDentry) {
	var (
		children []proto.Dentry
		ino      *proto.InodeInfo
		err      error
	)

	if !os.FileMode(dentry.Type).IsDir() {
		return
	}

	scanDentries := make([]*proto.ScanDentry, 0)
	totalChildDirNum := 0
	totalChildFileNum := 0
	marker := ""
	done := false

	span := getSpan(s.ctx).WithOperation("handleVerDelBreadthFirst")
	for !done {
		children, err = s.mw.ReadDirLimitForSnapShotClean(s.ctx, dentry.Inode, marker, uint64(defaultReadDirLimit), s.getTaskVerSeq(), false)
		if err != nil && err != syscall.ENOENT {
			span.Errorf("ReadDirLimitForSnapShotClean failed, parent[%v] maker[%v] verSeq[%v] err[%v]",
				dentry.Inode, marker, s.getTaskVerSeq(), err)
			atomic.AddInt64(&s.currentStat.ErrorSkippedNum, 1)
			return
		}
		span.Debugf("ReadDirLimitForSnapShotClean parent[%v] maker[%v] verSeq[%v] children[%v]",
			dentry.Inode, marker, s.getTaskVerSeq(), len(children))

		if err == syscall.ENOENT {
			span.Errorf("ReadDirLimitForSnapShotClean failed, parent[%v] maker[%v] verSeq[%v] err[%v]",
				dentry.Inode, marker, s.getTaskVerSeq(), err)
			break
		}

		if marker != "" {
			if len(children) >= 1 && marker == children[0].Name {
				if len(children) <= 1 {
					span.Debugf("ReadDirLimit_ll done, parent[%v] maker[%v] verSeq[%v] children[%v]",
						dentry.Inode, marker, s.getTaskVerSeq(), children)
					break
				} else {
					skippedChild := children[0]
					children = children[1:]
					span.Debugf("ReadDirLimit_ll skip last marker[%v], parent[%v] verSeq[%v] skippedName[%v]",
						marker, dentry.Inode, s.getTaskVerSeq(), skippedChild.Name)
				}
			}
		}

		for _, child := range children {
			childDentry := &proto.ScanDentry{
				ParentId: dentry.Inode,
				Name:     child.Name,
				Inode:    child.Inode,
				Type:     child.Type,
			}
			if os.FileMode(childDentry.Type).IsDir() {
				s.inodeChan.In <- childDentry
				totalChildDirNum++
				span.Debugf("push dir(parent[%v] child name[%v] ino[%v]) in channel",
					childDentry.ParentId, childDentry.Name, childDentry.Inode)
			} else {
				scanDentries = append(scanDentries, childDentry)
			}
		}

		for _, file := range scanDentries {
			if ino, err = s.mw.Delete_Ver_ll(s.ctx, file.ParentId, file.Name, false, s.getTaskVerSeq(), dentry.Path); err != nil {
				span.Errorf("Delete_Ver_ll failed, file(parent[%v] child name[%v]) verSeq[%v] err[%v]",
					file.ParentId, file.Name, s.getTaskVerSeq(), err)
				atomic.AddInt64(&s.currentStat.ErrorSkippedNum, 1)
				return
			} else {
				totalChildFileNum++
				span.Debugf("Delete_Ver_ll success, file(parent[%v] child name[%v]) verSeq[%v] ino[%v]",
					file.ParentId, file.Name, s.getTaskVerSeq(), ino)
				atomic.AddInt64(&s.currentStat.FileNum, 1)
				atomic.AddInt64(&s.currentStat.TotalInodeNum, 1)
			}
		}
		scanDentries = scanDentries[:0]
		childrenNr := len(children)
		if (marker == "" && childrenNr < defaultReadDirLimit) || (marker != "" && childrenNr+1 < defaultReadDirLimit) {
			span.Debugf("ReadDirLimit_ll done, parent[%v] total childrenNr[%v] marker[%v]",
				dentry.Inode, totalChildFileNum+totalChildDirNum, marker)
			done = true
		} else {
			marker = children[childrenNr-1].Name
			span.Debugf("ReadDirLimit_ll next marker[%v] parent[%v]", marker, dentry.Inode)
		}
	}
}

func (s *SnapshotScanner) DoneScanning() bool {
	getSpan(s.ctx).Debugf("inodeChan.Len(%v) rPoll.RunningNum(%v)", s.inodeChan.Len(), s.rPoll.RunningNum())
	return s.inodeChan.Len() == 0 && s.rPoll.RunningNum() == 0
}

func (s *SnapshotScanner) checkScanning(report bool) {
	dur := time.Second * time.Duration(scanCheckInterval)
	taskCheckTimer := time.NewTimer(dur)
	span := getSpan(s.ctx)
	for {
		select {
		case <-s.stopC:
			span.Debug("stop checking scan")
			return
		case <-taskCheckTimer.C:
			if s.DoneScanning() {
				taskCheckTimer.Stop()
				if report {
					t := time.Now()
					response := s.adminTask.Response.(*proto.SnapshotVerDelTaskResponse)
					if s.currentStat.ErrorSkippedNum > 0 {
						response.Status = proto.TaskFailed
					} else {
						response.Status = proto.TaskSucceeds
					}
					response.EndTime = &t
					response.Done = true
					response.ID = s.ID
					response.LcNode = s.lcnode.localServerAddr
					response.SnapshotVerDelTask = s.verDelReq.Task
					response.VolName = s.Volume
					response.VerSeq = s.getTaskVerSeq()
					response.FileNum = s.currentStat.FileNum
					response.DirNum = s.currentStat.DirNum
					response.TotalInodeNum = s.currentStat.TotalInodeNum
					response.ErrorSkippedNum = s.currentStat.ErrorSkippedNum
					s.lcnode.scannerMutex.Lock()
					s.Stop()
					delete(s.lcnode.snapshotScanners, s.ID)
					s.lcnode.scannerMutex.Unlock()

					s.lcnode.respondToMaster(s.ctx, s.adminTask)
					span.Infof("scan completed for task(%v)", s.adminTask)
				} else {
					span.Infof("first round scan completed for task(%v) without report", s.adminTask)
				}
				return
			}
			taskCheckTimer.Reset(dur)
		}
	}
}
