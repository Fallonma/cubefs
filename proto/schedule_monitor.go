package proto

const (
	// scheduler
	MonitorSchedulerLeaderHeartbeat              = "leaderHeartbeat"
	MonitorSchedulerFollowerHeartbeat            = "followerHeartbeat"
	MonitorSchedulerIdentityMonitor              = "identityMonitor"
	MonitorSchedulerWorkerManager                = "workerManager"
	MonitorSchedulerTaskManager                  = "taskManager"
	MonitorSchedulerExceptionTaskManager         = "exceptionTaskManager"
	MonitorSchedulerDispatchTask                 = "dispatchTask"
	MonitorSchedulerMoveTaskToHistory            = "moveTaskToHistory"
	MonitorSchedulerMoveExceptionWorkerToHistory = "moveExceptionWorkerToHistory"
	MonitorSchedulerFlowControlManager           = "flowControlManager"
	MonitorSchedulerMigrateThresholdManager      = "migrateThresholdManager"

	// hBase client
	MonitorHBaseSelectDPMetrics       = "hBaseSelectDPMetrics"
	MonitorHBaseAddTask               = "hBaseAddTask"
	MonitorHBaseCheckSparkTaskRunning = "hBaseCheckSparkTaskRunning"

	// master client
	MonitorMasterListSmartVolumes          = "listSmartVolumes"
	MonitorMasterFrozenDataPartition       = "frozenDataPartition"
	MonitorMasterDecommissionDataPartition = "decommissionDataPartition"
	MonitorMasterGetDataPartition          = "GetDataPartition"

	// mysql client
	MonitorMysqlGetLeader                      = "getLeader"
	MonitorMysqlGetMaxTerm                     = "getMaxTerm"
	MonitorMysqlUpdateLeaderHeartbeat          = "updateLeaderHeartbeat"
	MonitorMysqlAddElectTerm                   = "addElectTerm"
	MonitorMysqlAddTask                        = "AddTask"
	MonitorMysqlAllocateTask                   = "AllocateTask"
	MonitorMysqlUpdateTaskStatus               = "UpdateTaskStatus"
	MonitorMysqlUpdateTasksStatusViaSource     = "UpdateTasksStatusViaSource"
	MonitorMysqlUpdateTaskWorkerAddr           = "UpdateTaskWorkerAddr"
	MonitorMysqlUpdateTaskFailed               = "UpdateTaskFailed"
	MonitorMysqlUpdateTaskInfo                 = "UpdateTaskInfo"
	MonitorMysqlUpdateTaskUpdateTime           = "UpdateTaskUpdateTime"
	MonitorMysqlSelectTask                     = "SelectTask"
	MonitorMysqlSelectAllocatedTask            = "SelectAllocatedTask"
	MonitorMysqlSelectNotFinishedTask          = "SelectNotFinishedTask"
	MonitorMysqlSelectTasksWithType            = "SelectTasksWithType"
	MonitorMysqlSelectUnallocatedTasks         = "SelectUnallocatedTasks"
	MonitorMysqlSelectRunningTasks             = "SelectRunningTasks"
	MonitorMysqlSelectSucceedTasks             = "SelectSucceedTasks"
	MonitorMysqlSelectExceptionTasks           = "SelectExceptionTasks"
	MonitorMysqlSelectNotModifiedForLongTime   = "SelectNotModifiedForLongTime"
	MonitorMysqlDeleteTask                     = "DeleteTask"
	MonitorMysqlDeleteTasks                    = "DeleteTasks"
	MonitorMysqlDeleteTaskByVolumeAndId        = "DeleteTaskByVolumeAndId"
	MonitorMysqlCheckDPTaskExist               = "CheckDPTaskExist"
	MonitorMysqlCheckMPTaskExist               = "CheckMPTaskExist"
	MonitorMysqlSelectExceptionWorkerNodeTasks = "SelectExceptionWorkerNodeTasks"
	MonitorMysqlAddWorker                      = "AddWorker"
	MonitorMysqlUpdateWorkerHeartbeat          = "UpdateWorkerHeartbeat"
	MonitorMysqlSelectWorker                   = "SelectWorker"
	MonitorMysqlSelectWorkerNode               = "SelectWorkerNode"
	MonitorMysqlSelectWorkersAll               = "SelectWorkersAll"
	MonitorMysqlCheckWorkerExist               = "CheckWorkerExist"
	MonitorMysqlSelectExceptionWorkers         = "SelectExceptionWorkers"
	MonitorMysqlAddExceptionWorkersToHistory   = "AddExceptionWorkersToHistory"
	MonitorMysqlDeleteExceptionWorkers         = "DeleteExceptionWorkers"
	MonitorMysqlAddTaskToHistory               = "AddTaskToHistory"
	MonitorMysqlAddTasksToHistory              = "AddTasksToHistory"
	MonitorMysqlDeleteTaskHistory              = "DeleteTaskHistory"
	MonitorMysqlAddFlowControl                 = "AddFlowControl"
	MonitorMysqlModifyFlowControl              = "ModifyFlowControl"
	MonitorMysqlListFlowControl                = "ListFlowControls"
	MonitorMysqlSelectFlowControlsViaType      = "SelectFlowControlsViaType"
	MonitorMysqlDeleteFlowControl              = "DeleteFlowControl"
	MonitorMysqlAddScheduleConfig              = "AddScheduleConfig"
	MonitorMysqlSelectScheduleConfig           = "SelectScheduleConfig"
	MonitorMysqlUpdateScheduleConfig           = "UpdateScheduleConfig"
	MonitorMysqlDeleteScheduleConfig           = "DeleteScheduleConfig"

	// base worker
	MonitorWorkerHeartbeat = "workerHeartbeat"

	// smart volume worker

	MonitorSmartLoadSmartVolume = "loadSmartVolume"
	MonitorSmartCreateTask      = "smartVolumeCreateTask"
	MonitorSmartConsumeTask     = "smartVolumeConsumeTask"

	MonitorCompactLoadCompactVolume = "loadCompactVolume"
	MonitorCompactCreateTask        = "compactCreateTask"
	MonitorCompactConsumeTask       = "compactConsumeTask"

	MonitorSmartLoadSmartVolumeInode = "loadSmartVolumeInode"
	MonitorSmartCreateTaskInode      = "smartVolumeCreateTaskInode"
	MonitorSmartConsumeTaskInode     = "smartVolumeConsumeTaskInode"
)
