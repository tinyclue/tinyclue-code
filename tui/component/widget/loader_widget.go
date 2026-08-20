package widget

import (
	"sync"
	"time"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// LoaderState 表示加载挂件的状态。挂件只有运行中和完成两种状态，
// 没有 default 状态（运行中即默认形态）。
type LoaderState int

const (
	LoaderRunning LoaderState = iota // 运行中：Braille 旋转动画
	LoaderDone                       // 完成：绿色对勾
)

// LoaderWidget 是带旋转动画的状态挂件，参考 pi 的 loader.ts 组件。
// 运行中显示 Braille 旋转动画（每 80ms 切换一帧，参考 pi DEFAULT_FRAMES），
// 完成后显示静态对勾。实现了 Widget 接口。
type LoaderWidget struct {
	component.WidgetBase
	mu            sync.Mutex
	state         LoaderState
	frame         int
	frames        []string
	interval      time.Duration
	requestRender func()        // 触发 TUI 重绘的回调，动画每帧切换后调用
	stopCh        chan struct{} // 非 nil 表示动画协程正在运行
}

// NewLoaderWidget 创建一个加载挂件，初始为运行中状态。
func NewLoaderWidget() *LoaderWidget {
	return &LoaderWidget{
		state:    LoaderRunning,
		frames:   []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		interval: 80 * time.Millisecond,
	}
}

// SetRequestRender 设置触发 TUI 重绘的回调，由外部（tui_event.go）注入。
// 必须在 SetState(LoaderRunning) 之前调用，否则动画不会触发重绘。
func (w *LoaderWidget) SetRequestRender(fn func()) {
	w.mu.Lock()
	w.requestRender = fn
	w.mu.Unlock()
}

// SetState 设置挂件状态：运行中启动动画，完成时停止动画。
func (w *LoaderWidget) SetState(state LoaderState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = state
	if state == LoaderRunning {
		if w.stopCh != nil {
			return // 已在运行，不重启动画
		}
		w.frame = 0
		stop := make(chan struct{})
		w.stopCh = stop
		go w.animate(stop)
	} else {
		if w.stopCh != nil {
			close(w.stopCh)
			w.stopCh = nil
		}
	}
}

// State 返回当前挂件状态。
func (w *LoaderWidget) State() LoaderState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// animate 定时推进动画帧，并在每帧切换后触发重绘。
func (w *LoaderWidget) animate(stop chan struct{}) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.mu.Lock()
			w.frame = (w.frame + 1) % len(w.frames)
			rr := w.requestRender
			w.mu.Unlock()
			if rr != nil {
				rr()
			}
		}
	}
}

// Render 返回带 ANSI 颜色转义序列的当前帧字符。实现 Widget 接口。
// 运行中：粉色 Braille 旋转字符；完成：绿色对勾。
func (w *LoaderWidget) Render() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == LoaderDone {
		return types.FgGreen + "✓" + types.Reset
	}
	frame := w.frames[w.frame%len(w.frames)]
	return types.FgMagenta + frame + types.Reset
}

// Width 返回视觉宽度 1（Braille 与对勾均为单列字符）。
func (w *LoaderWidget) Width() int { return 1 }
