package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// dynamicMultiWriter 支持运行时动态添加 io.Writer 的多路输出器。
type dynamicMultiWriter struct {
	mu      sync.Mutex
	writers []io.Writer
}

func (dmw *dynamicMultiWriter) Write(p []byte) (int, error) {
	dmw.mu.Lock()
	writers := make([]io.Writer, len(dmw.writers))
	copy(writers, dmw.writers)
	dmw.mu.Unlock()

	var lastErr error
	for _, w := range writers {
		_, err := w.Write(p)
		if err != nil {
			lastErr = err
		}
	}
	return len(p), lastErr
}

func (dmw *dynamicMultiWriter) Add(w io.Writer) {
	dmw.mu.Lock()
	dmw.writers = append(dmw.writers, w)
	dmw.mu.Unlock()
}

// RotatingFileWriter 基于文件大小自动滚动的日志写入器。
// 当前文件超过 maxSize 后，会关闭并重命名为 path.1，已有备份依次后移，然后创建新文件。
type RotatingFileWriter struct {
	path    string
	maxSize int64
	backups int
	file    *os.File
	size    int64
	mu      sync.Mutex
}

// NewRotatingFileWriter 创建一个 RotatingFileWriter。
// backups 指定保留的滚动文件数量，小于 1 时默认保留 7 份。
func NewRotatingFileWriter(path string, maxSize int64, backups int) (*RotatingFileWriter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}

	if backups < 1 {
		backups = 7
	}

	return &RotatingFileWriter{
		path:    path,
		maxSize: maxSize,
		backups: backups,
		file:    f,
		size:    fi.Size(),
	}, nil
}

func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxSize && w.size > 0 {
		w.rotate()
	}

	if w.file == nil {
		return len(p), nil // rotation failed, drop silently
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingFileWriter) rotate() {
	w.file.Close()
	w.file = nil

	// 滚动备份文件：path.N → path.N+1
	for i := w.backups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", w.path, i)
		if _, err := os.Stat(old); err == nil {
			os.Rename(old, fmt.Sprintf("%s.%d", w.path, i+1))
		}
	}

	// 当前文件 → path.1
	os.Rename(w.path, w.path+".1")

	// 创建新文件
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	w.file = f
	w.size = 0
}

func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
