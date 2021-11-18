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

package repl

import (
	"container/list"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/util"
	"github.com/chubaofs/chubaofs/util/log"
	"github.com/tiglabs/raft/tracing"
)

var (
	gConnPool       = util.NewConnectPool()
	ReplProtocalMap sync.Map
)

// ReplProtocol defines the struct of the replication protocol.
// 1. ServerConn reads a packet from the client socket, and analyzes the addresses of the followers.
// 2. After the preparation, the packet is send to toBeProcessedCh. If failure happens, send it to the response channel.
// 3. OperatorAndForwardPktGoRoutine fetches a packet from toBeProcessedCh, and determine if it needs to be forwarded to the followers.
// 4. receiveResponse fetches a reply from responseCh, executes postFunc, and writes a response to the client if necessary.
type ReplProtocol struct {
	packetListLock sync.RWMutex

	packetList *list.List    // stores all the received packets from the client
	ackCh      chan struct{} // if sending to all the replicas succeeds, then a signal to this channel

	toBeProcessedCh chan *Packet // the goroutine receives an available packet and then sends it to this channel
	responseCh      chan *Packet // this chan is used to write response to the client

	sourceConn *net.TCPConn
	exitC      chan bool
	exited     int32
	exitedMu   sync.RWMutex

	followerConnects map[string]*FollowerTransport
	lock             sync.RWMutex

	prepareFunc  func(p *Packet) error                 // prepare packet
	operatorFunc func(p *Packet, c *net.TCPConn) error // operator
	postFunc     func(p *Packet) error                 // post-processing packet

	isError              int32
	replId               int64
	stopError            string
	startTime            int64
	allThreadStats       []int
	allThreadStatsLock   sync.Mutex
	getNumFromBufferPool int64
	putNumToBufferPool   int64
}

type FollowerTransport struct {
	addr           string
	conn           net.Conn
	sendCh         chan *FollowerPacket
	recvCh         chan *FollowerPacket
	exitCh         chan struct{}
	exitedMu       sync.RWMutex
	isclosed       int32
	lastActiveTime int64
}

func NewFollowersTransport(addr string) (ft *FollowerTransport, err error) {
	var (
		conn net.Conn
	)
	if conn, err = gConnPool.GetConnect(addr); err != nil {
		return
	}
	ft = new(FollowerTransport)
	ft.addr = addr
	ft.conn = conn
	ft.sendCh = make(chan *FollowerPacket, RequestChanSize)
	ft.recvCh = make(chan *FollowerPacket, RequestChanSize)
	ft.exitCh = make(chan struct{})
	ft.lastActiveTime = time.Now().Unix()
	go ft.serverWriteToFollower()
	go ft.serverReadFromFollower(context.Background())

	return
}

func (ft *FollowerTransport) PutRequestToRecvCh(request *FollowerPacket) (err error) {
	select {
	case ft.recvCh <- request:
		return
	default:
		return fmt.Errorf("FollowerTransport(%v) RecvCh has full", ft.addr)
	}
}

func (ft *FollowerTransport) PutRequestToSendCh(request *FollowerPacket) (err error) {
	select {
	case ft.sendCh <- request:
		return
	default:
		return fmt.Errorf("FollowerTransport(%v) SendCh has full", ft.addr)
	}
}

func (ft *FollowerTransport) serverWriteToFollower() {
	for {
		select {
		case p := <-ft.sendCh:
			atomic.StoreInt64(&ft.lastActiveTime, time.Now().Unix())
			if err := p.WriteToConn(ft.conn); err != nil {
				p.Data = nil
				p.respCh <- fmt.Errorf(ActionSendToFollowers+" follower(%v) error(%v)", ft.addr, err.Error())
				_ = ft.conn.Close()
				log.LogErrorf("request(%v) ActionSendToFollowers(%v) error(%v)", p.GetUniqueLogId(), ft.conn.RemoteAddr().String(), err.Error())
				continue
			}
			p.Data = nil
			if err := ft.PutRequestToRecvCh(p); err != nil {
				p.respCh <- fmt.Errorf(ActionSendToFollowers+" follower(%v) error(%v)", ft.addr, err.Error())
				_ = ft.conn.Close()
				log.LogErrorf("request(%v) ActionSendToFollowers(%v) error(%v)", p.GetUniqueLogId(), ft.conn.RemoteAddr().String(), err.Error())
				continue
			}

		case <-ft.exitCh:
			ft.exitedMu.Lock()
			if atomic.AddInt32(&ft.isclosed, -1) == FollowerTransportExited {
				_ = ft.conn.Close()
				atomic.StoreInt32(&ft.isclosed, FollowerTransportExited)
			}
			ft.exitedMu.Unlock()
			return
		}
	}
}

func (ft *FollowerTransport) serverReadFromFollower(ctx context.Context) {
	for {
		select {
		case p := <-ft.recvCh:
			atomic.StoreInt64(&ft.lastActiveTime, time.Now().Unix())
			_ = ft.readFollowerResult(ctx, p)
		case <-ft.exitCh:
			ft.exitedMu.Lock()
			if atomic.AddInt32(&ft.isclosed, -1) == FollowerTransportExited {
				_ = ft.conn.Close()
				atomic.StoreInt32(&ft.isclosed, FollowerTransportExited)
			}
			ft.exitedMu.Unlock()
			return
		}
	}
}

// Read the response from the follower
func (ft *FollowerTransport) readFollowerResult(ctx context.Context, request *FollowerPacket) (err error) {
	reply := NewPacket(ctx)
	defer func() {
		request.respCh <- err
		request.Data = nil
		if err != nil {
			_ = ft.conn.Close()
			log.LogErrorf("request(%v) readFollowerResult(%v) error(%v)", request.GetUniqueLogId(), ft.conn.RemoteAddr().String(), err)
		}
	}()
	request.Data = nil
	if err = reply.ReadFromConn(ft.conn, proto.ReadDeadlineTime); err != nil {
		return
	}

	if reply.ReqID != request.ReqID || reply.PartitionID != request.PartitionID ||
		reply.ExtentOffset != request.ExtentOffset || reply.CRC != request.CRC || reply.ExtentID != request.ExtentID {
		err = fmt.Errorf(ActionCheckReply+" request(%v), reply(%v)  ", request.GetUniqueLogId(),
			reply.GetUniqueLogId())
		return
	}

	if reply.IsErrPacket() {
		err = fmt.Errorf(string(reply.Data[:reply.Size]))
		return
	}
	log.LogDebugf("action[ActionReceiveFromFollower] %v.", reply.LogMessage(ActionReceiveFromFollower,
		ft.addr, request.StartT, err))
	return
}

func (ft *FollowerTransport) cleanSendChan() {
	for {
		select {
		case r := <-ft.sendCh:
			if r == nil {
				return
			}
			r.Data = nil
		default:
			return
		}
	}
}

func (ft *FollowerTransport) cleanRecvChan() {
	for {
		select {
		case r := <-ft.recvCh:
			if r == nil {
				return
			}
			r.Data = nil
		default:
			return
		}
	}
}

func (ft *FollowerTransport) Destory() {
	ft.exitedMu.Lock()
	atomic.StoreInt32(&ft.isclosed, FollowerTransportExiting)
	close(ft.exitCh)
	ft.exitedMu.Unlock()
	for {
		if atomic.LoadInt32(&ft.isclosed) == FollowerTransportExited {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ft.cleanSendChan()
	ft.cleanRecvChan()
	close(ft.sendCh)
	close(ft.recvCh)
}

func (ft *FollowerTransport) needAutoDestory() (release bool) {
	if time.Now().Unix()-atomic.LoadInt64(&ft.lastActiveTime) < FollowerTransportIdleTime {
		return false
	}
	ft.Destory()
	return true
}

func (ft *FollowerTransport) Write(p *FollowerPacket) (err error) {
	err = ft.PutRequestToSendCh(p)
	return
}

func NewReplProtocol(inConn *net.TCPConn, prepareFunc func(p *Packet) error,
	operatorFunc func(p *Packet, c *net.TCPConn) error, postFunc func(p *Packet) error) *ReplProtocol {
	rp := new(ReplProtocol)
	rp.packetList = list.New()
	rp.ackCh = make(chan struct{}, RequestChanSize)
	rp.toBeProcessedCh = make(chan *Packet, RequestChanSize)
	rp.responseCh = make(chan *Packet, RequestChanSize)
	rp.exitC = make(chan bool, 1)
	rp.sourceConn = inConn
	rp.followerConnects = make(map[string]*FollowerTransport)
	rp.prepareFunc = prepareFunc
	rp.operatorFunc = operatorFunc
	rp.startTime = time.Now().Unix()
	rp.postFunc = postFunc
	rp.allThreadStats = make([]int, 3)
	rp.exited = ReplRuning
	rp.replId = proto.GenerateRequestID()
	ReplProtocalMap.Store(rp.replId, rp)
	go rp.OperatorAndForwardPktGoRoutine()
	go rp.writeResponseToClientGoRroutine()

	return rp
}

const (
	ReplProtocalThreadRuning = 1
	ReplProtocalThreadExit   = -1
)

// ServerConn keeps reading data from the socket to analyze the follower address, execute the prepare function,
// and throw the packets to the to-be-processed channel.
func (rp *ReplProtocol) ServerConn() {
	var (
		err error
	)
	defer func() {
		rp.Stop(err)
		rp.exitedMu.Lock()
		rp.allThreadStatsLock.Lock()
		rp.allThreadStats[0] = ReplProtocalThreadExit
		rp.allThreadStatsLock.Unlock()
		if atomic.AddInt32(&rp.exited, -1) == ReplHasExited {
			_ = rp.sourceConn.Close()
			rp.cleanResource()
		}
		rp.exitedMu.Unlock()

	}()
	rp.allThreadStatsLock.Lock()
	rp.allThreadStats[0] = ReplProtocalThreadRuning
	rp.allThreadStatsLock.Unlock()
	for {
		select {
		case <-rp.exitC:
			return
		default:
			if err = rp.readPkgAndPrepare(); err != nil {
				return
			}
		}
	}

}

// Receive response from all followers.
//func (rp *ReplProtocol) ReceiveResponseFromFollowersGoRoutine() {
//	for {
//		select {
//		case <-rp.ackCh:
//			rp.checkLocalResultAndReciveAllFollowerResponse()
//		case <-rp.exitC:
//			rp.exitedMu.Lock()
//			if atomic.AddInt32(&rp.exited, -1) == ReplHasExited {
//				rp.sourceConn.Close()
//				rp.cleanResource()
//			}
//			rp.exitedMu.Unlock()
//			return
//		}
//	}
//}

func (rp *ReplProtocol) readPkgAndPrepare() (err error) {
	request := NewPacket(context.Background())
	var isUsedBufferPool bool
	isUsedBufferPool, err = request.ReadFromConnFromCli(rp.sourceConn, ReplProtocalServerTimeOut)
	if isUsedBufferPool {
		rp.addGetNumFromBufferPoolCnt()
	}
	if err != nil {
		err=fmt.Errorf("%v local(%v)->remote(%v) recive error(%v)",ActionreadPkgAndPrepare, rp.sourceConn.LocalAddr().String(),
			rp.sourceConn.RemoteAddr().String() ,err)
		return
	}
	request.OrgBuffer = request.Data
	if tracing.IsEnabled() {
		tracer := tracing.TracerFromContext(request.Ctx()).ChildTracer("repl.ReplProtocol.readPkgAndPrepare")
		defer tracer.Finish()
		request.SetCtx(tracer.Context())
	}
	remoteAddr := rp.sourceConn.RemoteAddr().String()
	log.LogDebugf("action[readPkgAndPrepare] packet(%v) from remote(%v) ",
		request.GetUniqueLogId(), remoteAddr)
	if err = request.resolveFollowersAddr(remoteAddr); err != nil {
		err = rp.putResponse(request)
		return
	}
	if err = rp.prepareFunc(request); err != nil {
		log.LogErrorf("%v  packet(%v) from remote(%v) error(%v)",
			ActionPreparePkt, request.GetUniqueLogId(), remoteAddr, err.Error())
		err = rp.putResponse(request)
		return
	}
	err = rp.putToBeProcess(request)

	return
}

func (rp *ReplProtocol) sendRequestToAllFollowers(request *Packet) (index int, err error) {
	if tracing.IsEnabled() {
		tracer := tracing.TracerFromContext(request.Ctx()).ChildTracer("repl.sendRequestToAllFollowers")
		defer tracer.Finish()
		request.SetCtx(tracer.Context())
	}
	for index = 0; index < len(request.followersAddrs); index++ {
		var transport *FollowerTransport
		if transport, err = rp.allocateFollowersConns(request, index); err != nil {
			request.PackErrorBody(ActionSendToFollowers, err.Error())
			return
		}
		followerRequest := NewFollowerPacket(request.Ctx())
		copyPacket(request, followerRequest)
		followerRequest.RemainingFollowers = 0
		request.followerPackets[index] = followerRequest
		err = transport.Write(followerRequest)
		if err != nil {
			request.PackErrorBody(ActionSendToFollowers, err.Error())
			return
		}
	}

	return
}

// OperatorAndForwardPktGoRoutine reads packets from the to-be-processed channel and writes responses to the client.
// 1. Read a packet from toBeProcessCh, and determine if it needs to be forwarded or not. If the answer is no, then
// 	  process the packet locally and put it into responseCh.
// 2. If the packet needs to be forwarded, the first send it to the followers, and execute the operator function.
//    Then notify receiveResponse to read the followers' responses.
// 3. Read a reply from responseCh, and write to the client.
func (rp *ReplProtocol) OperatorAndForwardPktGoRoutine() {
	ticker := time.NewTicker(time.Minute)
	defer func() {
		ticker.Stop()
	}()
	rp.allThreadStatsLock.Lock()
	rp.allThreadStats[1] = ReplProtocalThreadRuning
	rp.allThreadStatsLock.Unlock()
	for {
		select {
		case request := <-rp.toBeProcessedCh:
			if !request.IsForwardPacket() {
				_ = rp.operatorFunc(request, rp.sourceConn)
				_ = rp.putResponse(request)
			} else {
				_, err := rp.sendRequestToAllFollowers(request)
				if err != nil {
					_ = rp.putResponse(request)
				} else {
					rp.pushPacketToList(request)
					_ = rp.operatorFunc(request, rp.sourceConn)
					_ = rp.putAck(request)
				}
			}
		case <-ticker.C:
			rp.autoReleaseFollowerTransport()
		case <-rp.exitC:
			rp.exitedMu.Lock()
			rp.allThreadStatsLock.Lock()
			rp.allThreadStats[1] = ReplProtocalThreadExit
			rp.allThreadStatsLock.Unlock()
			if atomic.AddInt32(&rp.exited, -1) == ReplHasExited {
				_ = rp.sourceConn.Close()
				rp.cleanResource()
			}
			rp.exitedMu.Unlock()
			return
		}
	}

}

func (rp *ReplProtocol) autoReleaseFollowerTransport() {
	deleteTransportsKeys := make([]string, 0)
	rp.lock.Lock()
	if len(rp.followerConnects) == 0 {
		rp.lock.Unlock()
		return
	}
	for key, transport := range rp.followerConnects {
		release := transport.needAutoDestory()
		if release {
			deleteTransportsKeys = append(deleteTransportsKeys, key)
		}
	}
	for _, k := range deleteTransportsKeys {
		delete(rp.followerConnects, k)
	}
	rp.lock.Unlock()
}

func (rp *ReplProtocol) writeResponseToClientGoRroutine() {
	rp.allThreadStatsLock.Lock()
	rp.allThreadStats[2] = ReplProtocalThreadRuning
	rp.allThreadStatsLock.Unlock()
	for {
		select {
		case <-rp.ackCh:
			rp.checkLocalResultAndReciveAllFollowerResponse()
		case request := <-rp.responseCh:
			rp.writeResponse(request)
		case <-rp.exitC:
			rp.exitedMu.Lock()
			rp.allThreadStatsLock.Lock()
			rp.allThreadStats[2] = ReplProtocalThreadExit
			rp.allThreadStatsLock.Unlock()
			if atomic.AddInt32(&rp.exited, -1) == ReplHasExited {
				_ = rp.sourceConn.Close()
				rp.cleanResource()
			}
			rp.exitedMu.Unlock()
			return
		}
	}

}

type ReplProtocalBufferDetail struct {
	Addr     string
	Cnt      int64
	UseBytes int64
	ReplID   int64
}

func GetReplProtocolDetail() (allReplDetail []*ReplProtocalBufferDetail) {
	allReplDetail = make([]*ReplProtocalBufferDetail, 0)
	ReplProtocalMap.Range(func(key, value interface{}) bool {
		rp := value.(*ReplProtocol)
		if atomic.LoadInt64(&rp.getNumFromBufferPool) <= 0 {
			return true
		}
		rd := new(ReplProtocalBufferDetail)
		rd.Addr = rp.sourceConn.RemoteAddr().String()
		rd.Cnt = atomic.LoadInt64(&rp.getNumFromBufferPool) - atomic.LoadInt64(&rp.putNumToBufferPool)
		rd.ReplID = rp.replId
		rd.UseBytes = rd.Cnt * util.BlockSize
		allReplDetail = append(allReplDetail, rd)
		return true
	})
	return
}

func LoggingAllReplProtocolBufferPoolUse() {
	return
	//for {
	//	var (
	//		sumBytes int64
	//	)
	//	ReplProtocalMap.Range(func(key, value interface{}) bool {
	//		if value == nil {
	//			return true
	//		}
	//		rp := value.(*ReplProtocol)
	//		if atomic.LoadInt64(&rp.getNumFromBufferPool) <= 0 {
	//			return true
	//		}
	//		usedPoolCnt := atomic.LoadInt64(&rp.getNumFromBufferPool) - atomic.LoadInt64(&rp.putNumToBufferPool)
	//		sumBytes += (usedPoolCnt) * util.BlockSize
	//		return true
	//	})
	//	log.LogErrorf(fmt.Sprintf("ReplProtocalMap use (%v) from buffer pool", sumBytes))
	//	time.Sleep(time.Minute)
	//}

}

func (rp *ReplProtocol) operatorFuncWithWaitGroup(wg *sync.WaitGroup, request *Packet) {
	defer wg.Done()
	_ = rp.operatorFunc(request, rp.sourceConn)
}

// Read a packet from the list, scan all the connections of the followers of this packet and read the responses.
// If failed to read the response, then mark the packet as failure, and delete it from the list.
// If all the reads succeed, then mark the packet as success.
func (rp *ReplProtocol) checkLocalResultAndReciveAllFollowerResponse() {
	var (
		e *list.Element
	)

	if e = rp.getNextPacket(); e == nil {
		return
	}
	request := e.Value.(*Packet)
	defer func() {
		rp.deletePacket(request, e)
	}()
	if request.IsErrPacket() {
		return
	}
	for index := 0; index < len(request.followersAddrs); index++ {
		followerPacket := request.followerPackets[index]
		err := <-followerPacket.respCh
		followerPacket.Data = nil
		if err != nil {
			request.PackErrorBody(ActionReceiveFromFollower, err.Error())
			return
		}

	}
	return
}

// Write a reply to the client.
func (rp *ReplProtocol) writeResponse(reply *Packet) {
	var err error
	defer func() {
		rp.cleanPacket(reply)
	}()

	_ = rp.postFunc(reply)
	if !reply.NeedReply {
		return
	}
	if reply.IsErrPacket() {
		err = fmt.Errorf(reply.LogMessage(ActionWriteToClient, rp.sourceConn.RemoteAddr().String(),
			reply.StartT, fmt.Errorf(string(reply.Data[:reply.Size]))))
		log.LogErrorf(err.Error())
	}

	if err = reply.WriteToConn(rp.sourceConn); err != nil {
		err = fmt.Errorf(reply.LogMessage(ActionWriteToClient, fmt.Sprintf("local(%v)->remote(%v)", rp.sourceConn.LocalAddr().String(),
			rp.sourceConn.RemoteAddr().String()), reply.StartT, err))
		log.LogErrorf(err.Error())
		rp.Stop(err)
	}
	log.LogDebugf(reply.LogMessage(ActionWriteToClient,
		rp.sourceConn.RemoteAddr().String(), reply.StartT, err))
}

// Stop stops the replication protocol.
func (rp *ReplProtocol) Stop(stopErr error) {
	rp.exitedMu.Lock()
	defer rp.exitedMu.Unlock()
	if stopErr != nil && rp.stopError == "" {
		rp.stopError = stopErr.Error()
	}
	if atomic.LoadInt32(&rp.exited) == ReplRuning {
		if rp.exitC != nil {
			close(rp.exitC)
		}
		atomic.StoreInt32(&rp.exited, ReplExiting)
	}

}

// Allocate the connections to the followers. We use partitionId + extentId + followerAddr as the key.
// Note that we need to ensure the order of packets sent to the datanode is consistent here.
func (rp *ReplProtocol) allocateFollowersConns(p *Packet, index int) (transport *FollowerTransport, err error) {
	rp.lock.RLock()
	transport = rp.followerConnects[p.followersAddrs[index]]
	if transport != nil {
		atomic.StoreInt64(&transport.lastActiveTime, time.Now().Unix())
	}
	rp.lock.RUnlock()
	if transport == nil {
		transport, err = NewFollowersTransport(p.followersAddrs[index])
		if err != nil {
			return
		}
		rp.lock.Lock()
		rp.followerConnects[p.followersAddrs[index]] = transport
		rp.lock.Unlock()
	}

	return
}

func (rp *ReplProtocol) getNextPacket() (e *list.Element) {
	rp.packetListLock.RLock()
	e = rp.packetList.Front()
	rp.packetListLock.RUnlock()

	return
}

func (rp *ReplProtocol) pushPacketToList(e *Packet) {
	rp.packetListLock.Lock()
	rp.packetList.PushBack(e)
	rp.packetListLock.Unlock()
}

func (rp *ReplProtocol) cleanToBeProcessCh() {
	for {
		select {
		case p := <-rp.toBeProcessedCh:
			if p == nil {
				return
			}
			_ = rp.postFunc(p)
			rp.cleanPacket(p)
			log.LogErrorf("Action[cleanToBeProcessCh] request(%v) because (%v)", p.GetUniqueLogId(), rp.stopError)
		default:
			return
		}
	}
}

func (rp *ReplProtocol) cleanResponseCh() {
	for {
		select {
		case p := <-rp.responseCh:
			if p == nil {
				return
			}
			_ = rp.postFunc(p)
			rp.cleanPacket(p)
			log.LogErrorf("Action[cleanResponseCh] request(%v) because (%v)", p.GetUniqueLogId(), rp.stopError)
		default:
			return
		}
	}
}

func (rp *ReplProtocol) loggingIsAllThreadsExit() {
	allExit := true
	var threadStat [3]int
	rp.allThreadStatsLock.Lock()
	for index, stat := range rp.allThreadStats {
		threadStat[index] = stat
	}
	rp.allThreadStatsLock.Unlock()

	for _, stat := range threadStat {
		if stat != ReplProtocalThreadExit {
			allExit = false
			return
		}
	}

	if allExit {
		return
	}
	log.LogErrorf("ReplProtocol(%v) not only allThreads  exit threadStats(%v)", rp.sourceConn.RemoteAddr(), threadStat)
}

func (rp *ReplProtocol) cleanPacket(p *Packet) {
	var ok bool
	if ok = p.clean(); ok {
		rp.addPutNumFromBufferPoolCnt()
		return
	}
	if p.IsWriteOperation() && p.OrgSize == util.BlockSize {
		log.LogErrorf("request(%v) not return to pool, packet is UseBufferPool(%v)",
			p.LogMessage("ActionCleanToPacket", rp.sourceConn.RemoteAddr().String(), p.StartT, nil), p.isUseBufferPool)
	}
}

func (rp *ReplProtocol) addGetNumFromBufferPoolCnt() {
	atomic.AddInt64(&rp.getNumFromBufferPool, 1)
}

func (rp *ReplProtocol) addPutNumFromBufferPoolCnt() {
	atomic.AddInt64(&rp.putNumToBufferPool, 1)
}

// If the replication protocol exits, then clear all the packet resources.
func (rp *ReplProtocol) cleanResource() {
	rp.loggingIsAllThreadsExit()
	rp.lock.RLock()
	for _, transport := range rp.followerConnects {
		transport.Destory()
	}
	rp.lock.RUnlock()

	rp.packetListLock.Lock()
	for e := rp.packetList.Front(); e != nil; e = e.Next() {
		request := e.Value.(*Packet)
		_ = rp.postFunc(request)
		log.LogErrorf("Action[cleanResource] request(%v) because (%v)", request.GetUniqueLogId(), rp.stopError)
		rp.cleanPacket(request)
	}
	rp.cleanToBeProcessCh()
	rp.cleanResponseCh()
	if atomic.LoadInt64(&rp.getNumFromBufferPool) != atomic.LoadInt64(&rp.putNumToBufferPool) {
		log.LogErrorf("ReplProtocol(%v) use buffer pool error,"+
			"getNumFromBufferPool(%v) putNumToBufferPool(%v)", rp.sourceConn.RemoteAddr(),
			atomic.LoadInt64(&rp.getNumFromBufferPool), atomic.LoadInt64(&rp.putNumToBufferPool))
	}
	rp.packetList = list.New()
	close(rp.responseCh)
	close(rp.toBeProcessedCh)
	close(rp.ackCh)
	rp.packetList = nil
	rp.followerConnects = nil
	rp.packetListLock.Unlock()
	ReplProtocalMap.Delete(rp.replId)
}

func (rp *ReplProtocol) deletePacket(reply *Packet, e *list.Element) (success bool) {
	rp.packetListLock.Lock()
	defer rp.packetListLock.Unlock()
	rp.packetList.Remove(e)
	success = true
	_ = rp.putResponse(reply)
	return
}

func (rp *ReplProtocol) putResponse(reply *Packet) (err error) {
	select {
	case rp.responseCh <- reply:
		return
	default:
		err = fmt.Errorf("request(%v) response Chan has full (%v) ", reply.GetUniqueLogId(), len(rp.responseCh))
		log.LogErrorf(err.Error())
		return err
	}
}

func (rp *ReplProtocol) putToBeProcess(request *Packet) (err error) {
	select {
	case rp.toBeProcessedCh <- request:
		return
	default:
		err = fmt.Errorf("request(%v)  toBeProcessedCh Chan has full (%v)", request.GetUniqueLogId(), len(rp.toBeProcessedCh))
		log.LogErrorf(err.Error())
		return err
	}
}

func (rp *ReplProtocol) putAck(request *Packet) (err error) {
	select {
	case rp.ackCh <- struct{}{}:
		return
	default:
		err = fmt.Errorf("request(%v) ack Chan has full (%v)", request.GetUniqueLogId(), len(rp.ackCh))
		log.LogErrorf(err.Error())
		return err
	}
}
