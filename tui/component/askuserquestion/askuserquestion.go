// Package askuserquestion provides a TUI panel for multi-tab question/answer interaction.
package askuserquestion

import (
	"fmt"
	"strings"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// QuestionOptionData represents a single option in a question.
type QuestionOptionData struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	//Preview     string `json:"preview,omitempty"`
}

// QuestionData represents a question with its options.
type QuestionData struct {
	Question    string               `json:"question"`
	Header      string               `json:"header"`
	Options     []QuestionOptionData `json:"options"`
	MultiSelect bool                 `json:"multiSelect,omitempty"`
}

// AnswersMap maps question text -> answer string.
// For multi-select, the answer is comma-separated option labels.
type AnswersMap map[string]string

// Panel is a tabbed TUI panel for displaying questions and collecting answers.
// Tabs: Q1 | Q2 | ... | QN | Submit
type Panel struct {
	questions  []QuestionData
	currentTab int // 0..len(questions); len(questions) = submit tab
	focusO     int // focused option index within current question

	selPerQ      []int    // per-question selected option index (single-select, -1 = none)
	toggledPerQ  [][]bool // per-question per-option toggle state (multi-select)
	otherText    []string // per-question "Other" text input content
	otherEditing bool     // whether currently editing an "Other" field

	onSubmit func(answers AnswersMap)
	onCancel func()

	*component.ComponentBase
}

// New creates a new AskUserQuestion panel.
func New(questions []QuestionData) *Panel {
	selPerQ := make([]int, len(questions))
	toggledPerQ := make([][]bool, len(questions))
	otherText := make([]string, len(questions))
	for qi, q := range questions {
		selPerQ[qi] = -1                                 // nothing selected by default
		toggledPerQ[qi] = make([]bool, len(q.Options)+1) // +1 for "Other"
	}

	return &Panel{
		questions:     questions,
		currentTab:    0,
		focusO:        0,
		selPerQ:       selPerQ,
		toggledPerQ:   toggledPerQ,
		otherText:     otherText,
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit registers the submit callback with the results.
func (p *Panel) OnSubmit(fn func(answers AnswersMap)) {
	p.onSubmit = fn
}

// OnCancel registers the cancel callback.
func (p *Panel) OnCancel(fn func()) {
	p.onCancel = fn
}

// DoBefore implements core.Component.
func (p *Panel) DoBefore(core.Data) error { return nil }

// DoUpdate implements core.Component.
func (p *Panel) DoUpdate(data core.Data) error {
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		p.handleInput(m)
	}
	return nil
}

func (p *Panel) handleInput(key core.KeyPressMsg) {
	if len(p.questions) == 0 {
		return
	}

	// Handle "Other" field editing mode
	if p.otherEditing {
		switch {
		case key.MatchString("enter"):
			p.otherEditing = false
			p.nextTab()
		case key.MatchString("esc"):
			p.otherEditing = false
		case key.MatchString("backspace", "ctrl+h"):
			text := p.otherText[p.currentTab]
			if len(text) > 0 {
				runes := []rune(text)
				p.otherText[p.currentTab] = string(runes[:len(runes)-1])
			}
		case key.MatchString("up", "ctrl+p"):
			p.otherEditing = false
			if p.focusO > 0 {
				p.focusO--
			} else {
				p.focusO = len(p.questions[p.currentTab].Options) // wrap to Other
			}
		case key.MatchString("down", "ctrl+n"):
			p.otherEditing = false
			maxOpt := len(p.questions[p.currentTab].Options)
			p.focusO++
			if p.focusO > maxOpt {
				p.focusO = 0
			}
		default:
			if k := key.Key(); k.Text != "" {
				p.otherText[p.currentTab] += k.Text
			}
		}
		return
	}

	// Not editing — handle navigation and selection.
	// Gate: handle submit-tab separately to avoid out-of-bounds on questions slice.
	if p.isOnSubmitTab() {
		switch {
		case key.MatchString("left"):
			p.prevTab()
		case key.MatchString("right"):
			p.nextTab()
		case key.MatchString("enter"):
			if p.onSubmit != nil {
				p.onSubmit(p.buildAnswers())
			}
		case key.MatchString("esc"):
			if p.onCancel != nil {
				p.onCancel()
			}
		}
		return
	}

	// Must be on a question tab now — safe to access questions[p.currentTab].
	q := p.questions[p.currentTab]
	maxOpt := len(q.Options) // "Other" is at this index

	switch {
	case key.MatchString("left"):
		p.prevTab()

	case key.MatchString("right"):
		p.nextTab()

	case key.MatchString("up", "ctrl+p"):
		p.focusO--
		if p.focusO < 0 {
			p.focusO = maxOpt // wrap to Other
		}

	case key.MatchString("down", "ctrl+n"):
		p.focusO++
		if p.focusO > maxOpt { // past Other
			p.focusO = 0
		}

	case key.MatchString(" "):
		if q.MultiSelect {
			p.toggledPerQ[p.currentTab][p.focusO] = !p.toggledPerQ[p.currentTab][p.focusO]
		}

	case key.MatchString("enter"):
		if p.focusO == maxOpt {
			// "Other" option — enter edit mode
			p.otherEditing = true
			if !q.MultiSelect {
				p.selPerQ[p.currentTab] = p.focusO
			} else {
				p.toggledPerQ[p.currentTab][p.focusO] = true
			}
		} else {
			if !q.MultiSelect {
				p.selPerQ[p.currentTab] = p.focusO
			}
			p.nextTab()
		}

	case key.MatchString("esc"):
		if p.onCancel != nil {
			p.onCancel()
		}
	}
}

func (p *Panel) prevTab() {
	if p.currentTab > 0 {
		p.currentTab--
		p.focusO = 0
	}
}

func (p *Panel) nextTab() {
	if p.currentTab < len(p.questions) {
		p.currentTab++
		p.focusO = 0
	}
}

func (p *Panel) isOnQuestionTab() bool {
	return p.currentTab >= 0 && p.currentTab < len(p.questions)
}

func (p *Panel) isOnSubmitTab() bool {
	return p.currentTab == len(p.questions)
}

func (p *Panel) isAnswered(qi int) bool {
	q := p.questions[qi]
	if q.MultiSelect {
		for _, toggled := range p.toggledPerQ[qi] {
			if toggled {
				return true
			}
		}
		return false
	}
	if p.selPerQ[qi] == len(q.Options) {
		// "Other" selected — only answered if text was entered
		return p.otherText[qi] != ""
	}
	return p.selPerQ[qi] >= 0
}

// buildAnswers constructs the answers map from current panel state.
func (p *Panel) buildAnswers() AnswersMap {
	answers := make(AnswersMap, len(p.questions))
	for qi, q := range p.questions {
		otherIdx := len(q.Options) // index of the "Other" option
		if q.MultiSelect {
			var selected []string
			for oi, toggled := range p.toggledPerQ[qi] {
				if toggled && oi < otherIdx {
					selected = append(selected, q.Options[oi].Label)
				}
			}
			if p.toggledPerQ[qi][otherIdx] && p.otherText[qi] != "" {
				selected = append(selected, "Other: "+p.otherText[qi])
			}
			if len(selected) > 0 {
				answers[q.Question] = strings.Join(selected, ", ")
			}
		} else {
			if p.selPerQ[qi] >= 0 && p.selPerQ[qi] < len(q.Options) {
				answers[q.Question] = q.Options[p.selPerQ[qi]].Label
			} else if p.selPerQ[qi] == otherIdx && p.otherText[qi] != "" {
				answers[q.Question] = "Other: " + p.otherText[qi]
			}
		}
	}
	return answers
}

// GetOtherText returns the "Other" text input for the given question index.
func (p *Panel) GetOtherText(qi int) string {
	if qi >= 0 && qi < len(p.otherText) {
		return p.otherText[qi]
	}
	return ""
}

// Render implements core.Component.
func (p *Panel) Render(data core.Data) core.View {
	width := terminal.DefaultTerminalContext.GetWidth()
	width = max(width, 10)

	var lines []string

	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")

	// Tab navigation bar
	p.renderTabBar(&lines, width)

	// Content area
	if p.isOnQuestionTab() {
		p.renderQuestionTab(&lines)
	} else if p.isOnSubmitTab() {
		p.renderSubmitTab(&lines)
	}

	return core.View{Lines: lines}
}

func (p *Panel) renderTabBar(lines *[]string, width int) {
	var tabs []string

	// Left arrow
	if p.currentTab > 0 {
		tabs = append(tabs, types.FgGray+"←"+types.Reset)
	}

	// Question tabs
	for qi := range p.questions {
		answered := p.isAnswered(qi)
		header := p.questions[qi].Header
		if header == "" {
			header = fmt.Sprintf("Q%d", qi+1)
		}

		check := "☐"
		if answered {
			check = types.FgGreen + "☑" + types.Reset
		}

		tab := fmt.Sprintf("%s %s", check, header)
		if qi == p.currentTab {
			tab = types.FgLightBlue + types.Bold + tab + types.Reset
		}
		tabs = append(tabs, tab)
	}

	// Submit tab
	submitLabel := types.FgGreen + "✓ Submit" + types.Reset
	if p.isOnSubmitTab() {
		submitLabel = types.FgLightBlue + types.Bold + "✓ Submit" + types.Reset
	}
	tabs = append(tabs, submitLabel)

	// Right arrow
	if !p.isOnSubmitTab() {
		tabs = append(tabs, types.FgGray+"→"+types.Reset)
	}

	tabLine := strings.Join(tabs, "  ")
	if len(tabLine) > width {
		tabLine = tabLine[:width]
	}
	*lines = append(*lines, "  "+tabLine)
	*lines = append(*lines, "")
}

func (p *Panel) renderQuestionTab(lines *[]string) {
	q := p.questions[p.currentTab]

	// Header
	header := q.Header
	if header == "" {
		header = fmt.Sprintf("Question %d", p.currentTab+1)
	}
	*lines = append(*lines, fmt.Sprintf("  %s%s%s", types.Bold, header, types.Reset))

	// Question text
	if q.Question != "" {
		*lines = append(*lines, fmt.Sprintf("  %s", q.Question))
	}
	*lines = append(*lines, "")

	// Options
	for oi, opt := range q.Options {
		focused := oi == p.focusO

		// Prefix: ❯ for focused, spaces otherwise
		var styleOn, styleOff string
		prefix := "    "
		if focused {
			prefix = "  " + types.FgLightBlue + "❯" + types.Reset + " "
			styleOn = types.FgLightBlue + types.Bold
			styleOff = types.Reset
		}

		// Indicator: checkbox for multi-select, radio for single-select
		var indicator string
		if q.MultiSelect {
			if p.toggledPerQ[p.currentTab][oi] {
				indicator = types.FgGreen + "☑" + types.Reset + " "
			} else {
				indicator = "☐ "
			}
		} else {
			if p.selPerQ[p.currentTab] == oi {
				indicator = types.FgGreen + "◉" + types.Reset + " "
			} else {
				indicator = "○ "
			}
		}

		line := fmt.Sprintf("%s%s%s%s", prefix, styleOn, indicator, opt.Label)
		if opt.Description != "" {
			line += strings.Repeat(" ", 3) + types.FgGray + opt.Description + types.Reset
		}
		line += styleOff
		*lines = append(*lines, line)
	}

	// "Other" option (virtual option at index len(q.Options))
	otherIdx := len(q.Options)
	otherFocused := p.focusO == otherIdx
	otherText := p.otherText[p.currentTab]

	var oprefix string
	var ostyleOn, ostyleOff string
	if otherFocused {
		oprefix = "  " + types.FgLightBlue + "❯" + types.Reset + " "
		ostyleOn = types.FgLightBlue + types.Bold
		ostyleOff = types.Reset
	} else {
		oprefix = "    "
	}

	var oindicator string
	if q.MultiSelect {
		if p.toggledPerQ[p.currentTab][otherIdx] {
			oindicator = types.FgGreen + "☑" + types.Reset + " "
		} else {
			oindicator = "☐ "
		}
	} else {
		if p.selPerQ[p.currentTab] == otherIdx {
			oindicator = types.FgGreen + "◉" + types.Reset + " "
		} else {
			oindicator = "○ "
		}
	}

	displayText := otherText
	if p.otherEditing && otherFocused {
		// Reverse-video block cursor matching editor's CursorStyle
		if displayText == "" {
			displayText = types.CursorStyle + " " + types.Reset
		} else {
			displayText = displayText + types.CursorStyle + " " + types.Reset
		}
	} else if displayText == "" {
		displayText = "___________"
	}

	oline := fmt.Sprintf("%s%s%sOther: [%s]%s", oprefix, ostyleOn, oindicator, displayText, ostyleOff)
	// Operation hint after the Other option
	if otherFocused {
		if p.otherEditing {
			oline += types.FgGray + "  type · backspace · enter confirm · esc cancel" + types.Reset
		} else {
			oline += types.FgGray + "  enter to type" + types.Reset
		}
	}
	*lines = append(*lines, oline)
	*lines = append(*lines, "")

	// Footer hint
	if q.MultiSelect {
		*lines = append(*lines, types.FgGray+"  ↑↓ navigate · space toggle · → next · enter confirm"+types.Reset)
	} else {
		*lines = append(*lines, types.FgGray+"  ↑↓ navigate · → next · enter select & confirm"+types.Reset)
	}
	*lines = append(*lines, "")
}

func (p *Panel) renderSubmitTab(lines *[]string) {
	// Title
	*lines = append(*lines, fmt.Sprintf("  %sReview your answers%s", types.Bold, types.Reset))
	*lines = append(*lines, "")

	// Check for unanswered questions
	allAnswered := true
	for qi, q := range p.questions {
		if !p.isAnswered(qi) {
			allAnswered = false
			*lines = append(*lines, fmt.Sprintf("  %s⚠ You have not answered: %s%s",
				types.FgLightRed, q.Question, types.Reset))
		}
	}
	if !allAnswered {
		*lines = append(*lines, "")
	}

	// Answer summary
	answers := p.buildAnswers()
	for _, q := range p.questions {
		if ans, ok := answers[q.Question]; ok {
			*lines = append(*lines, fmt.Sprintf("  %s•%s %s", types.FgGreen, types.Reset, q.Question))
			*lines = append(*lines, fmt.Sprintf("    %s→%s %s", types.FgGray, types.Reset, ans))
		} else {
			*lines = append(*lines, fmt.Sprintf("  %s•%s %s", types.FgGray, types.Reset, q.Question))
			*lines = append(*lines, fmt.Sprintf("    %s(unanswered)%s", types.FgGray, types.Reset))
		}
	}
	*lines = append(*lines, "")

	// Submit prompt
	*lines = append(*lines, types.FgGray+"  ← navigate tabs · enter submit · esc cancel"+types.Reset)
	*lines = append(*lines, "")
}
