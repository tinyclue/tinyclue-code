// Package component 提供可折叠组件的独立嵌入基类和折叠规则。
package component

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// CollapseRule 定义折叠规则：从完整 lines 中提取折叠后需要保留的部分。
type CollapseRule interface {
	Apply(lines []string) []string
}

type headRule struct{ n int }

func (r *headRule) Apply(lines []string) []string {
	if len(lines) <= r.n {
		return lines
	}
	return lines[:r.n]
}

type tailRule struct{ n int }

func (r *tailRule) Apply(lines []string) []string {
	if len(lines) <= r.n {
		return lines
	}
	return lines[len(lines)-r.n:]
}

// ShowHead 返回只保留开头 N 行的折叠规则。
func ShowHead(n int) CollapseRule { return &headRule{n} }

// ShowTail 返回只保留末尾 N 行的折叠规则。
func ShowTail(n int) CollapseRule { return &tailRule{n} }

// Collapsible 定义组件折叠能力接口。
type Collapsible interface {
	IsCollapsible() bool
	IsCollapsed() bool
	SetCollapsed(bool)
	ApplyCollapse(lines []string) []string
}

// CollapsibleBase 是折叠能力的独立嵌入基类。
// 任何组件只需嵌入 *CollapsibleBase 并在 Render() 末尾调用 ApplyCollapse 即可获得折叠能力。
type CollapsibleBase struct {
	collapsible bool
	collapsed   bool
	rule        CollapseRule
	summaryTmpl string
}

// NewCollapsibleBase 创建折叠基类，默认折叠状态为 false（不折叠）、摘要模板指向默认提示。
func NewCollapsibleBase() *CollapsibleBase {
	return &CollapsibleBase{
		summaryTmpl: "    ... (%d lines folded, Ctrl+O expand, Esc fold)",
	}
}

// SetCollapsible 启用折叠能力并设置折叠规则。调用后组件默认处于折叠状态。
func (b *CollapsibleBase) SetCollapsible(rule CollapseRule) {
	b.collapsible = true
	b.collapsed = true
	b.rule = rule
}

// SetSummary 自定义折叠后追加的摘要模板，格式同 fmt.Sprintf，%d 接收折叠行数。
func (b *CollapsibleBase) SetSummary(tmpl string) {
	b.summaryTmpl = tmpl
}

// IsCollapsible 返回组件是否启用了折叠能力。
func (b *CollapsibleBase) IsCollapsible() bool { return b.collapsible }

// IsCollapsed 返回组件当前是否处于折叠状态。
func (b *CollapsibleBase) IsCollapsed() bool { return b.collapsed }

// SetCollapsed 设置组件的折叠状态。
func (b *CollapsibleBase) SetCollapsed(v bool) { b.collapsed = v }

// ApplyCollapse 对 lines 执行折叠截断：当组件可折叠且处于折叠状态且行数超过规则限制时，
// 截断并追加灰色摘要行。否则直接返回原切片。
func (b *CollapsibleBase) ApplyCollapse(lines []string) []string {
	if !b.collapsible || !b.collapsed || b.rule == nil {
		return lines
	}
	kept := b.rule.Apply(lines)
	if len(kept) >= len(lines) {
		return lines
	}
	kept = append(kept,
		types.FgGray+fmt.Sprintf(b.summaryTmpl, len(lines)-len(kept))+types.Reset)
	return kept
}
