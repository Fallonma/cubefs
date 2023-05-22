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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cubefs/cubefs/codecnode"
	"github.com/cubefs/cubefs/convertnode"
	"github.com/cubefs/cubefs/ecnode"
	"github.com/cubefs/cubefs/flashnode"
	"github.com/cubefs/cubefs/schedulenode/checkcrc"
	"github.com/cubefs/cubefs/schedulenode/checktool"
	"github.com/cubefs/cubefs/schedulenode/compact"
	"github.com/cubefs/cubefs/schedulenode/rebalance"
	"github.com/cubefs/cubefs/schedulenode/scheduler"
	"github.com/cubefs/cubefs/schedulenode/smart"
	syslog "log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cubefs/cubefs/monitor"

	"github.com/cubefs/cubefs/cmd/common"
	"github.com/cubefs/cubefs/console"
	"github.com/cubefs/cubefs/datanode"
	"github.com/cubefs/cubefs/master"
	"github.com/cubefs/cubefs/metanode"
	"github.com/cubefs/cubefs/objectnode"
	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/util/config"
	"github.com/cubefs/cubefs/util/log"
	sysutil "github.com/cubefs/cubefs/util/sys"

	"github.com/cubefs/cubefs/util/version"
	"github.com/jacobsa/daemonize"
)

const (
	ConfigKeyRole       = "role"
	ConfigKeyLogDir     = "logDir"
	ConfigKeyLogLevel   = "logLevel"
	ConfigKeyProfPort   = "prof"
	ConfigKeyWarnLogDir = "warnLogDir"
)

const (
	RoleMaster    = "master"
	RoleMeta      = "metanode"
	RoleData      = "datanode"
	RoleFlash     = "flashnode"
	RoleObject    = "objectnode"
	RoleConsole   = "console"
	RoleMonitor   = "monitor"
	RoleConvert   = "convert"
	RoleSchedule  = "schedulenode"
	RoleSmart     = "smartvolume"
	RoleCompact   = "compact"
	RoleCheckCrc = "checkcrc"
	RoleCodec     = "codecnode"
	RoleEc        = "ecnode"
	RoleReBalance = "rebalance"
	RoleChecktool = "checktool"
)

const (
	ModuleMaster    = "master"
	ModuleMeta      = "metaNode"
	ModuleData      = "dataNode"
	ModuleFlash     = "flashNode"
	ModuleObject    = "objectNode"
	ModuleConsole   = "console"
	ModuleMonitor   = "monitor"
	ModuleConvert   = "convert"
	ModuleSchedule  = "scheduleNode"
	ModuleSmart     = "smartVolume"
	ModuleCompact   = "compact"
	ModuleCheckCrc = "checkcrc"
	ModuleCodec     = "codecNode"
	ModuleEc        = "ecNode"
	ModuleReBalance = "rebalance"
	ModuleChecktool = "checktool"
)

const (
	LoggerOutput = "output.log"
)

const (
	MaxThread = 40000
)

const (
	HTTPAPIPATHStatus = "/status"
)

var (
	startComplete         = false
	startCost             time.Duration
	HttpAPIPathSetTracing = "/tracing/set"
	HttpAPIPathGetTracing = "/tracing/get"
)

var (
	CommitID   string
	BranchName string
	BuildTime  string
)

var (
	configFile       = flag.String("c", "", "config file path")
	configVersion    = flag.Bool("v", false, "show version")
	configForeground = flag.Bool("f", false, "run foreground")
)

func interceptSignal(s common.Server) {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	syslog.Println("action[interceptSignal] register system signal.")
	go func() {
		sig := <-sigC
		syslog.Printf("action[interceptSignal] received signal: %s.", sig.String())
		s.Shutdown()
	}()
}

func modifyOpenFiles() (err error) {
	var rLimit syscall.Rlimit
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Getting Rlimit %v", err.Error())
	}
	syslog.Println(rLimit)
	rLimit.Max = 1024000
	rLimit.Cur = 1024000
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Setting Rlimit %v", err.Error())
	}
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("Error Getting Rlimit %v", err.Error())
	}
	syslog.Println("Rlimit Final", rLimit)
	return
}

func modifyMaxThreadLimit() {
	debug.SetMaxThreads(MaxThread)
}

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	Version := proto.DumpVersion("Server", BranchName, CommitID, BuildTime)
	if *configVersion {
		fmt.Printf("%v", Version)
		return nil
	}

	/*
	 * LoadConfigFile should be checked before start daemon, since it will
	 * call os.Exit() w/o notifying the parent process.
	 */
	cfg, err := config.LoadConfigFile(*configFile)
	if err != nil {
		_ = daemonize.SignalOutcome(err)
		return err
	}

	if !*configForeground {
		if err := startDaemon(); err != nil {
			fmt.Printf("Server start failed: %v\n", err)
			return err
		}
		return nil
	}

	/*
	 * We are in daemon from here.
	 * Must notify the parent process through SignalOutcome anyway.
	 */

	role := cfg.GetString(ConfigKeyRole)
	logDir := cfg.GetString(ConfigKeyLogDir)
	logLevel := cfg.GetString(ConfigKeyLogLevel)
	profPort := cfg.GetString(ConfigKeyProfPort)

	//Init tracing

	// Init server instance with specified role configuration.
	var (
		server common.Server
		module string
	)
	switch role {
	case RoleMeta:
		server = metanode.NewServer()
		module = ModuleMeta
	case RoleMaster:
		server = master.NewServer()
		module = ModuleMaster
	case RoleData:
		server = datanode.NewServer()
		module = ModuleData
	case RoleFlash:
		server = flashnode.NewServer()
		module = ModuleFlash
	case RoleObject:
		server = objectnode.NewServer()
		module = ModuleObject
	case RoleConsole:
		server = console.NewServer()
		module = ModuleConsole
	case RoleMonitor:
		server = monitor.NewServer()
		module = ModuleMonitor
	case RoleConvert:
		server = convertnode.NewServer()
		module = ModuleConvert
	case RoleSchedule:
		server = scheduler.NewScheduleNode()
		module = ModuleSchedule
	case RoleSmart:
		server = smart.NewSmartVolumeWorker()
		module = ModuleSmart
	case RoleCompact:
		server = compact.NewCompactWorker()
		module = ModuleCompact
	case RoleCheckCrc:
		server = checkcrc.NewCrcWorker()
		module = ModuleCheckCrc
	case RoleCodec:
		server = codecnode.NewServer()
		module = ModuleCodec
	case RoleEc:
		server = ecnode.NewServer()
		module = ModuleEc
	case RoleReBalance:
		server = rebalance.NewReBalanceWorker()
		module = ModuleReBalance
	case RoleChecktool:
		server = checktool.NewChecktoolWorker()
		module = ModuleChecktool

	default:
		_ = daemonize.SignalOutcome(fmt.Errorf("Fatal: role mismatch: %v", role))
		return fmt.Errorf("unknown role: %v", role)
	}

	// Init logging
	var (
		level log.Level
	)
	switch strings.ToLower(logLevel) {
	case "debug":
		level = log.DebugLevel
	case "info":
		level = log.InfoLevel
	case "warn":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	default:
		level = log.ErrorLevel
	}

	_, err = log.InitLog(logDir, module, level, nil)
	if err != nil {
		_ = daemonize.SignalOutcome(fmt.Errorf("Fatal: failed to init log - %v", err))
		return err
	}
	defer log.LogFlush()

	// Init output file
	outputFilePath := path.Join(logDir, module, LoggerOutput)
	outputFile, err := os.OpenFile(outputFilePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		_ = daemonize.SignalOutcome(err)
		return err
	}
	defer func() {
		outputFile.Sync()
		outputFile.Close()
	}()
	syslog.SetOutput(outputFile)

	if err = sysutil.RedirectFD(int(outputFile.Fd()), int(os.Stderr.Fd())); err != nil {
		log.LogErrorf("redirect stderr to %v failed: %v", outputFilePath, err)
		log.LogFlush()
		syslog.Printf("Fatal: redirect stderr to %v failed: %v", outputFilePath, err)
		_ = daemonize.SignalOutcome(fmt.Errorf("refiect stderr to %v failed: %v", outputFilePath, err))
		return err
	}

	syslog.Printf("Hello, ChubaoFS Storage\n%s\n", Version)

	err = modifyOpenFiles()
	if err != nil {
		log.LogErrorf("modify open files limit failed: %v", err)
		log.LogFlush()
		syslog.Printf("Fatal: modify open files limit failed: %v ", err)
		_ = daemonize.SignalOutcome(fmt.Errorf("modify open files limit failed: %v", err))
		return err
	}
	// Setup thread limit from 10,000 to 40,000
	modifyMaxThreadLimit()

	//for multi-cpu scheduling
	runtime.GOMAXPROCS(runtime.NumCPU())

	var profNetListener net.Listener = nil
	if profPort != "" {
		http.HandleFunc(HTTPAPIPATHStatus, statusHandler)
		// 监听prof端口
		if profNetListener, err = net.Listen("tcp", fmt.Sprintf(":%v", profPort)); err != nil {
			log.LogErrorf("listen prof port %v failed: %v", profPort, err)
			log.LogFlush()
			syslog.Printf("Fatal: listen prof port %v failed: %v", profPort, err)
			_ = daemonize.SignalOutcome(fmt.Errorf("listen prof port %v failed: %v", profPort, err))
			return err
		}
		// 在prof端口监听上启动http API.
		go func() {
			_ = http.Serve(profNetListener, http.DefaultServeMux)
		}()
	}

	interceptSignal(server)

	var startTime = time.Now()
	if err = server.Start(cfg); err != nil {
		log.LogErrorf("start service %v failed: %v", role, err)
		log.LogFlush()
		syslog.Printf("Fatal: failed to start the ChubaoFS %v daemon err %v - ", role, err)
		_ = daemonize.SignalOutcome(fmt.Errorf("Fatal: failed to start the ChubaoFS %v daemon err %v - ", role, err))
		return err
	}
	startComplete = true
	startCost = time.Since(startTime)

	_ = daemonize.SignalOutcome(nil)

	// report server version
	masters := cfg.GetStringSlice(proto.MasterAddr)
	versionInfo := proto.DumpVersion(module, BranchName, CommitID, BuildTime)
	port, _ := strconv.ParseUint(profPort, 10, 64)
	go version.ReportVersionSchedule(cfg, masters, versionInfo, "", "", CommitID, port, nil, nil)

	// Block main goroutine until server shutdown.
	server.Sync()
	log.LogFlush()

	if profNetListener != nil {
		// 关闭prof端口监听
		_ = profNetListener.Close()
	}

	return nil
}

func startDaemon() error {
	cmdPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("startDaemon failed: cannot get absolute command path, err(%v)", err)
	}

	configPath, err := filepath.Abs(*configFile)
	if err != nil {
		return fmt.Errorf("startDaemon failed: cannot get absolute command path of config file(%v) , err(%v)", *configFile, err)
	}

	args := []string{"-f"}
	args = append(args, "-c")
	args = append(args, configPath)

	env := []string{
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
	}

	err = daemonize.Run(cmdPath, args, env, os.Stdout)
	if err != nil {
		return fmt.Errorf("startDaemon failed: daemon start failed, cmd(%v) args(%v) env(%v) err(%v)\n", cmdPath, args, env, err)
	}

	return nil
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var resp = struct {
		StartComplete bool
		StarCost      time.Duration
		Version       string
	}{
		StartComplete: startComplete,
		StarCost:      startCost,
		Version:       proto.Version,
	}
	if marshaled, err := json.Marshal(&resp); err == nil {
		_, _ = w.Write(marshaled)
	}
}
