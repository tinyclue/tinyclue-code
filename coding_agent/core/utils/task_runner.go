package utils

import "sync"

// TaskRunner 收集异步任务并并发执行，返回第一个错误。
func NewTaskRunner() *TaskRunner {
	return &TaskRunner{}
}

type TaskRunner struct {
	tasks []func() error
}

// Add 注册一个返回 (error, T) 的任务，结果写入 dst。
func Add[T any](r *TaskRunner, dst *T, fn func() (error, T)) {
	r.tasks = append(r.tasks, func() error {
		err, v := fn()
		if err != nil {
			return err
		}
		*dst = v
		return nil
	})
}

// Run 并发执行所有注册的任务，返回第一个非 nil 的错误。
func (r *TaskRunner) Run() error {
	n := len(r.tasks)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return r.tasks[0]()
	}

	errCh := make(chan error, n)
	var wg sync.WaitGroup

	for _, t := range r.tasks {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			if err := fn(); err != nil {
				errCh <- err
			}
		}(t)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}
