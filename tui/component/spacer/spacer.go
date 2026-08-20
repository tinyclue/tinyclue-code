// Package spacer 提供空行组件 Spacer。
package spacer

import (
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
)

// Spacer 渲染指定数量的空行。
type Spacer struct {
	*component.ComponentBase
	lines int
}

// New 创建一个 Spacer，lines 指定空行数。
func New(lines ...int) *Spacer {
	n := 1
	if len(lines) > 0 {
		n = lines[0]
	}
	return &Spacer{
		ComponentBase: component.NewComponentBase(),
		lines:         n,
	}
}

// SetLines 更新空行数。
func (s *Spacer) SetLines(lines int) {
	s.lines = lines
}

func (s *Spacer) DoBefore(_ core.Data) error {
	return nil
}

func (s *Spacer) DoUpdate(_ core.Data) error {
	return nil
}

// Render 返回指定数量的空行。
func (s *Spacer) Render(_ core.Data) core.View {
	lines := make([]string, s.lines)
	for i := range lines {
		lines[i] = ""
	}
	return core.View{Lines: lines}
}
