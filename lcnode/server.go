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
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cubefs/cubefs/blobstore/common/trace"
	"github.com/cubefs/cubefs/cmd/common"
	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/sdk/master"
	"github.com/cubefs/cubefs/util"
	"github.com/cubefs/cubefs/util/config"
	"github.com/cubefs/cubefs/util/errors"
	"github.com/cubefs/cubefs/util/exporter"
	"github.com/cubefs/cubefs/util/log"
	"golang.org/x/time/rate"
)

//TODO: remove this later.
//go:generate golangci-lint run --issues-exit-code=1 -D errcheck -E bodyclose ./...

var (
	getSpan     = proto.SpanFromContext
	spanContext = proto.SpanContext
)

func spanContextOperation(op string) (trace.Span, context.Context) {
	span, ctx := spanContext()
	span = span.WithOperation(op)
	ctx = proto.ContextWithSpan(ctx, span)
	return span, ctx
}

type LcNode struct {
	listen           string
	localServerAddr  string
	clusterID        string
	nodeID           uint64
	masters          []string
	mc               *master.MasterClient
	scannerMutex     sync.RWMutex
	stopC            chan bool
	lastHeartbeat    time.Time
	control          common.Control
	lcScanners       map[string]*LcScanner
	snapshotScanners map[string]*SnapshotScanner
}

func NewServer() *LcNode {
	return &LcNode{
		lcScanners:       make(map[string]*LcScanner),
		snapshotScanners: make(map[string]*SnapshotScanner),
	}
}

func (l *LcNode) Start(cfg *config.Config) (err error) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	return l.control.Start(l, cfg, doStart)
}

func (l *LcNode) Shutdown() {
	l.control.Shutdown(l, doShutdown)
}

func (l *LcNode) Sync() {
	l.control.Sync()
}

func doStart(s common.Server, cfg *config.Config) (err error) {
	l, ok := s.(*LcNode)
	if !ok {
		return errors.New("Invalid node Type!")
	}
	l.stopC = make(chan bool)

	span, ctx := spanContextOperation("start")
	if err = l.parseConfig(ctx, cfg); err != nil {
		return
	}
	l.register(ctx)
	l.lastHeartbeat = time.Now()

	go l.checkRegister()
	if err = l.startServer(ctx); err != nil {
		return
	}

	exporter.Init(ModuleName, cfg)
	exporter.RegistConsul(l.clusterID, ModuleName, cfg)

	span.Info("lcnode start successfully")
	return
}

func doShutdown(s common.Server) {
	l, ok := s.(*LcNode)
	if !ok {
		return
	}
	l.stopServer()
}

func (l *LcNode) parseConfig(ctx context.Context, cfg *config.Config) (err error) {
	span := getSpan(ctx).WithOperation("loadConfig")
	// parse listen
	listen := cfg.GetString(configListen)
	if len(listen) == 0 {
		listen = defaultListen
	}
	if match := regexpListen.MatchString(listen); !match {
		err = errors.New("invalid listen configuration")
		return
	}
	l.listen = listen
	span.Infof("setup config: %v(%v)", configListen, listen)

	// parse master config
	masters := cfg.GetStringSlice(configMasterAddr)
	if len(masters) == 0 {
		return config.NewIllegalConfigError(configMasterAddr)
	}
	span.Infof("setup config: %v(%v)", configMasterAddr, strings.Join(masters, ","))
	l.masters = masters
	l.mc = master.NewMasterClient(masters, false)

	// parse batchExpirationGetNum
	begns := cfg.GetString(configBatchExpirationGetNumStr)
	var batchNum int64
	if begns != "" {
		if batchNum, err = strconv.ParseInt(begns, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}
	batchExpirationGetNum = int(batchNum)
	if batchExpirationGetNum <= 0 || batchExpirationGetNum > maxBatchExpirationGetNum {
		batchExpirationGetNum = defaultBatchExpirationGetNum
	}
	span.Infof("setup config: %v(%v)", configBatchExpirationGetNumStr, batchExpirationGetNum)

	// parse scanCheckInterval
	scis := cfg.GetString(configScanCheckIntervalStr)
	if scis != "" {
		if scanCheckInterval, err = strconv.ParseInt(scis, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}
	if scanCheckInterval <= 0 {
		scanCheckInterval = defaultScanCheckInterval
	}
	span.Infof("setup config: %v(%v)", configScanCheckIntervalStr, scanCheckInterval)

	// parse lcScanRoutineNumPerTask
	var routineNum int64
	lcScanRoutineNum := cfg.GetString(configLcScanRoutineNumPerTaskStr)
	if lcScanRoutineNum != "" {
		if routineNum, err = strconv.ParseInt(lcScanRoutineNum, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}
	lcScanRoutineNumPerTask = int(routineNum)
	if lcScanRoutineNumPerTask <= 0 || lcScanRoutineNumPerTask > maxLcScanRoutineNumPerTask {
		lcScanRoutineNumPerTask = defaultLcScanRoutineNumPerTask
	}
	span.Infof("setup config: %v(%v)", configLcScanRoutineNumPerTaskStr, lcScanRoutineNumPerTask)

	// parse snapshotRoutineNumPerTask
	routineNum = 0
	snapRoutineNum := cfg.GetString(configSnapshotRoutineNumPerTaskStr)
	if snapRoutineNum != "" {
		if routineNum, err = strconv.ParseInt(snapRoutineNum, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}

	snapshotRoutineNumPerTask = int(routineNum)
	if snapshotRoutineNumPerTask <= 0 || snapshotRoutineNumPerTask > maxLcScanRoutineNumPerTask {
		snapshotRoutineNumPerTask = defaultLcScanRoutineNumPerTask
	}
	span.Infof("setup config: %v(%v)", configSnapshotRoutineNumPerTaskStr, snapshotRoutineNumPerTask)

	// parse lcScanLimitPerSecond
	var limitNum int64
	lcScanLimit := cfg.GetString(configLcScanLimitPerSecondStr)
	if lcScanLimit != "" {
		if limitNum, err = strconv.ParseInt(lcScanLimit, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}
	if limitNum <= 0 {
		lcScanLimitPerSecond = defaultLcScanLimitPerSecond
	} else {
		lcScanLimitPerSecond = rate.Limit(limitNum)
	}
	span.Infof("setup config: %v(%v)", configLcScanLimitPerSecondStr, lcScanLimitPerSecond)

	// parse lcNodeTaskCount
	var count int64
	countStr := cfg.GetString(configLcNodeTaskCountLimit)
	if countStr != "" {
		if count, err = strconv.ParseInt(countStr, 10, 64); err != nil {
			return fmt.Errorf("%v,err:%v", proto.ErrInvalidCfg, err.Error())
		}
	}
	if count <= 0 || count > maxLcNodeTaskCountLimit {
		lcNodeTaskCountLimit = defaultLcNodeTaskCountLimit
	} else {
		lcNodeTaskCountLimit = int(count)
	}
	span.Infof("setup config: %v(%v)", configLcNodeTaskCountLimit, lcNodeTaskCountLimit)
	return
}

func (l *LcNode) register(ctx context.Context) {
	var err error
	timer := time.NewTimer(0)

	span := getSpan(ctx)
	// get the IsIPV4 address, cluster ID and node ID from the master
	for {
		select {
		case <-timer.C:
			var ci *proto.ClusterInfo
			if ci, err = l.mc.AdminAPI().GetClusterInfo(ctx); err != nil {
				span.Errorf("cannot get ip from master(%v) err(%v).", l.mc.Leader(), err)
				timer.Reset(2 * time.Second)
				continue
			}
			masterAddr := l.mc.Leader()
			l.clusterID = ci.Cluster
			localIP := ci.Ip
			l.localServerAddr = fmt.Sprintf("%s:%v", localIP, l.listen)
			if !util.IsIPV4(localIP) {
				span.Errorf("got an invalid local ip(%v) from master(%v).", localIP, masterAddr)
				timer.Reset(2 * time.Second)
				continue
			}

			// register this lcnode on the master
			var nodeID uint64
			if nodeID, err = l.mc.NodeAPI().AddLcNode(ctx, l.localServerAddr); err != nil {
				span.Errorf("cannot register this node to master[%v] err(%v).", masterAddr, err)
				timer.Reset(2 * time.Second)
				continue
			}
			l.nodeID = nodeID
			span.Infof("register: register LcNode: nodeID(%v)", l.nodeID)
			return
		case <-l.stopC:
			timer.Stop()
			return
		}
	}
}

func (l *LcNode) checkRegister() {
	for {
		span, ctx := spanContextOperation("check")
		if time.Since(l.lastHeartbeat) > time.Minute*10 {
			span.Warn("lcnode might be deregistered from master, stop scanners...")
			l.stopScanners()
			span.Warn("lcnode might be deregistered from master, retry registering...")
			l.register(ctx)
			l.lastHeartbeat = time.Now()
		}
		time.Sleep(time.Minute)
	}
}

func (l *LcNode) startServer(ctx context.Context) (err error) {
	span := getSpan(ctx)
	span.Info("start ...")
	addr := fmt.Sprintf(":%v", l.listen)
	listener, err := net.Listen("tcp", addr)
	span.Debugf("listen tcp address(%v).", addr)
	if err != nil {
		span.Error("failed to listen, err:", err)
		return
	}
	go func(stopC chan bool) {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			log.Debugf("accept connection from %s.", conn.RemoteAddr().String())
			select {
			case <-stopC:
				return
			default:
			}
			if err != nil {
				log.Errorf("failed to accept, err:%s", err.Error())
				continue
			}
			go l.serveConn(conn, stopC)
		}
	}(l.stopC)
	return
}

func (l *LcNode) serveConn(conn net.Conn, stopC chan bool) {
	defer conn.Close()
	c := conn.(*net.TCPConn)
	c.SetKeepAlive(true)
	c.SetNoDelay(true)
	remoteAddr := conn.RemoteAddr().String()
	for {
		select {
		case <-stopC:
			return
		default:
		}
		p := &proto.Packet{}
		if err := p.ReadFromConn(conn, proto.NoReadDeadlineTime); err != nil {
			if err != io.EOF {
				p.Span().Errorf("serveConn ReadFromConn err: %v", err)
			}
			return
		}
		span := p.Span()
		if err := l.handlePacket(conn, p, remoteAddr); err != nil {
			p.Span().Errorf("serveConn handlePacket err: %v", err)
		}
		if tracks := span.TrackLog(); len(tracks) > 0 {
			span.Info("tracks:", tracks)
		}
	}
}

func (l *LcNode) handlePacket(conn net.Conn, p *proto.Packet, remoteAddr string) (err error) {
	p.Span().Infof("HandleMetadataOperation input info op (%s), remote %s", p.String(), remoteAddr)
	switch p.Opcode {
	case proto.OpLcNodeHeartbeat:
		err = l.opMasterHeartbeat(conn, p, remoteAddr)
	case proto.OpLcNodeScan:
		err = l.opLcScan(conn, p)
	case proto.OpLcNodeSnapshotVerDel:
		err = l.opSnapshotVerDel(conn, p)
	default:
		err = fmt.Errorf("%s unknown Opcode: %d, reqId: %d", remoteAddr, p.Opcode, p.GetReqID())
	}
	if err != nil {
		err = errors.NewErrorf("%s [%s] req: %d - %s", remoteAddr, p.GetOpMsg(),
			p.GetReqID(), err.Error())
	}
	return
}

func (l *LcNode) stopServer() {
	if l.stopC != nil {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("action[StopTcpServer],err:%v", r)
			}
		}()
		close(l.stopC)
		log.Info("LcNode Stop!")
	}
}

func (l *LcNode) stopScanners() {
	l.scannerMutex.Lock()
	defer l.scannerMutex.Unlock()
	for _, s := range l.lcScanners {
		s.Stop()
		delete(l.lcScanners, s.ID)
	}
	for _, s := range l.snapshotScanners {
		s.Stop()
		delete(l.snapshotScanners, s.ID)
	}
}
