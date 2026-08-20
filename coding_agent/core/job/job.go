package job

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Job struct {
	mu        sync.RWMutex
	jobs      map[string]*types.JobState
	EventSink types.EventSink
}

var JobInstance = &Job{}

func (j *Job) Register(jobId string, jobType types.JobType, desc string, abort func(), sourceID string, data interface{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.jobs == nil {
		j.jobs = make(map[string]*types.JobState)
	}
	jobState := &types.JobState{
		ID:             jobId,
		Type:           jobType,
		Status:         types.JobStatusPending,
		Description:    desc,
		StartTime:      time.Now(),
		Notified:       false,
		Error:          "",
		OutputFilePath: GetJobOutputPath(jobId),
		Abort:          abort,
		SourceID:       sourceID,
		Data:           data,
	}
	j.jobs[jobId] = jobState
	j.EventSink.Emit(types.JobUpdateEventType, jobId, nil)
}

func (j *Job) Update(jobId string, fn func(job *types.JobState)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.jobs[jobId]
	if !ok {
		return
	}
	oldStatus := job.Status
	fn(job)
	if !isValidTransition(oldStatus, job.Status) {
		job.Status = oldStatus
		return
	}
	if job.Status == types.JobStatusCompleted || job.Status == types.JobStatusFailed || job.Status == types.JobStatusKilled {
		job.EndTime = time.Now()
	}
	j.EventSink.Emit(types.JobUpdateEventType, jobId, nil)
}

func isValidTransition(old, new types.JobStatus) bool {
	if old == new {
		return true
	}
	switch old {
	case types.JobStatusPending:
		return new == types.JobStatusRunning || new == types.JobStatusKilled
	case types.JobStatusRunning:
		return new == types.JobStatusCompleted || new == types.JobStatusFailed || new == types.JobStatusKilled
	default:
		return false
	}
}

// ReadAll 遍历所有注册的作业，fn 在读锁保护下执行。
func (j *Job) ReadAll(fn func(jobs map[string]*types.JobState)) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	fn(j.jobs)
}

// Read 是线程安全的只读访问方法，fn 在读锁保护下执行。
func (j *Job) Read(jobId string, fn func(job *types.JobState)) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if job, ok := j.jobs[jobId]; ok {
		fn(job)
	}
}

// GetJobOutputPath 返回 job 输出文件的路径。
func GetJobOutputPath(jobId string) string {
	return filepath.Join(os.TempDir(), "tinyclue", "jobs", jobId+".output")
}

// InitJobOutputAsSymlink 创建 outputFilePath -> sessionFilePath 的软链接。
func InitJobOutputAsSymlink(jobId, sessionFilePath string) error {
	outputPath := GetJobOutputPath(jobId)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	// 清理已存在的文件/链接
	os.Remove(outputPath)
	return os.Symlink(sessionFilePath, outputPath)
}
