package types

import (
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"time"
)

type JobType string

const (
	LOCAL_BASH  JobType = "local_bash"
	LOCAL_AGENT JobType = "local_agent"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusKilled    JobStatus = "killed"
)

type JobState struct {
	ID             string
	Type           JobType
	Status         JobStatus
	Description    string
	StartTime      time.Time
	EndTime        time.Time
	Notified       bool
	Error          string
	OutputFilePath string
	Abort          func()
	SourceID       string
	Data           interface{}
}

type AgentProgress struct {
	ToolUseCount       int
	TokenCount         int
	RecentTools        []apitypes.ToolCall
	LastActivesSummary string
}

// ToolProgress 后台 bash 任务的实时进度数据（最近输出行），供 UI 渲染，对齐 AgentProgress。
type ToolProgress struct {
	RecentLines []string // 最近输出行（最多 5 行）
}

type LocalSubAgentJobState struct {
	AgentId         string
	Prompt          string
	AgentType       AgentType
	Result          SubAgentContent
	Progress        AgentProgress
	PendingMessages []string
	ToolUseId       string
	IsAsync         bool
}

// LocalBashJobState 后台 bash 任务（run_in_background）的状态数据，供 UI 渲染与状态回填。
type LocalBashJobState struct {
	Command   string
	ToolUseId string
	ExitCode  int
	IsAsync   bool
	Progress  ToolProgress    // 实时进度（最近输出行）
	Result    BashTaskContent // 后台任务最终结果（终态时填充）
}
