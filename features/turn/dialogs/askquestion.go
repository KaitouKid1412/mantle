package dialogs

import (
	"encoding/json"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Question is one entry of AskUserQuestion's input.questions.
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
}

// QuestionOption is one choice of a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     Text   `json:"preview,omitempty"`
}

const askDenyMessage = "The user dismissed the questions without answering."

// AskQuestion is the view-model of an AskUserQuestion prompt: one tab per question,
// single or multiple choice, a free-text "Other" row, option previews, and a review tab
// when there is more than one answer to confirm.
type AskQuestion struct {
	req   ToolRequest
	ctx   PermissionContext
	input map[string]json.RawMessage
	qs    []Question

	tab     int   // question index; len(qs) is the review tab
	cursor  []int // per question: option index, len(options) = Other, len(options)+1 = Next (multi)
	picked  []map[int]bool
	other   []textField
	otherOn []bool
	editing bool
	review  optionList

	result *PermissionResult
}

// NewAskQuestion builds the prompt; it fails when input.questions is missing or empty.
func NewAskQuestion(req ToolRequest, ctx PermissionContext) (*AskQuestion, error) {
	a := &AskQuestion{req: req, ctx: ctx}
	if err := json.Unmarshal(req.Input, &a.input); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(a.input["questions"], &a.qs); err != nil {
		return nil, err
	}
	if len(a.qs) == 0 {
		return nil, errors.New("AskUserQuestion: no questions")
	}
	n := len(a.qs)
	a.cursor = make([]int, n)
	a.picked = make([]map[int]bool, n)
	a.other = make([]textField, n)
	a.otherOn = make([]bool, n)
	for i := range a.qs {
		a.picked[i] = map[int]bool{}
		a.other[i] = textField{placeholder: "Type your answer"}
	}
	a.review = optionList{items: []option{{id: "submit", label: "Submit answers"}, {id: "cancel", label: "Cancel"}}}
	return a, nil
}

// Done reports whether the prompt has been answered.
func (a *AskQuestion) Done() bool { return a.result != nil }

// SetPosition implements Positioner.
func (a *AskQuestion) SetPosition(index, total int) { a.ctx.Index, a.ctx.Total = index, total }

// Response is the answer to send; nil until Done.
func (a *AskQuestion) Response() *PermissionResult { return a.result }

// Deny answers with a dismissal.
func (a *AskQuestion) Deny() *PermissionResult {
	if a.result == nil {
		a.result = &PermissionResult{Behavior: "deny", Message: askDenyMessage, ToolUseID: a.req.ToolUseID, DecisionClassification: ClassUserReject}
	}
	return a.result
}

// Answers returns the answers map as the engine expects it: question text → label,
// "a, b" for several labels, or the free text. Unanswered questions are absent.
func (a *AskQuestion) Answers() map[string]string {
	out := map[string]string{}
	for i, q := range a.qs {
		if ans, ok := a.answer(i); ok {
			out[q.Question] = ans
		}
	}
	return out
}

func (a *AskQuestion) answer(i int) (string, bool) {
	q := a.qs[i]
	var parts []string
	for j, o := range q.Options {
		if a.picked[i][j] {
			parts = append(parts, o.Label)
		}
	}
	other := strings.TrimSpace(a.other[i].Value())
	if a.otherOn[i] && other != "" {
		parts = append(parts, other)
	}
	if len(parts) == 0 {
		return "", false
	}
	if !q.MultiSelect {
		return parts[len(parts)-1], true
	}
	return strings.Join(parts, ", "), true
}

func (a *AskQuestion) submit() (bool, Effect) {
	for i := range a.qs {
		if _, ok := a.answer(i); !ok {
			a.tab = i
			return true, None
		}
	}
	in := map[string]json.RawMessage{}
	for k, v := range a.input {
		in[k] = v
	}
	answers, _ := json.Marshal(a.Answers())
	in["answers"] = answers
	updated, _ := json.Marshal(in)
	a.result = &PermissionResult{
		Behavior:               "allow",
		UpdatedInput:           updated,
		ToolUseID:              a.req.ToolUseID,
		DecisionClassification: ClassUserTemporary,
	}
	return true, Answered
}

// advance moves past an answered question: to the next one, straight to submit for a
// lone single-choice question, or to the review tab.
func (a *AskQuestion) advance() (bool, Effect) {
	a.editing = false
	if a.tab < len(a.qs)-1 {
		a.tab++
		return true, None
	}
	if len(a.qs) == 1 && !a.qs[0].MultiSelect {
		return a.submit()
	}
	a.tab = len(a.qs)
	a.review.cursor = 0
	return true, None
}

func (a *AskQuestion) rows(i int) int {
	n := len(a.qs[i].Options) + 1
	if a.qs[i].MultiSelect {
		n++
	}
	return n
}

// activate acts on the row under the cursor (enter / space / digit).
func (a *AskQuestion) activate(toggle bool) (bool, Effect) {
	i, c := a.tab, a.cursor[a.tab]
	q := a.qs[i]
	switch {
	case c < len(q.Options):
		if q.MultiSelect {
			a.picked[i][c] = !a.picked[i][c]
			return true, None
		}
		a.picked[i] = map[int]bool{c: true}
		a.otherOn[i] = false
		if toggle {
			return true, None
		}
		return a.advance()
	case c == len(q.Options):
		a.editing = true
		return true, None
	default: // Next row of a multi-select question
		return a.advance()
	}
}

func (a *AskQuestion) commitOther() (bool, Effect) {
	i := a.tab
	text := strings.TrimSpace(a.other[i].Value())
	a.editing = false
	if text == "" {
		a.otherOn[i] = false
		return true, None
	}
	a.otherOn[i] = true
	if a.qs[i].MultiSelect {
		return true, None
	}
	a.picked[i] = map[int]bool{}
	return a.advance()
}

func (a *AskQuestion) switchTab(d int) {
	a.editing = false
	last := len(a.qs)
	if len(a.qs) == 1 && !a.qs[0].MultiSelect {
		last = 0
	}
	a.tab = min(max(a.tab+d, 0), last)
}

// HandleKey implements Model.
func (a *AskQuestion) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if a.Done() {
		return false, None
	}
	if a.editing {
		switch k.Keystroke() {
		case "enter":
			return a.commitOther()
		case "esc":
			a.editing = false
			a.otherOn[a.tab] = strings.TrimSpace(a.other[a.tab].Value()) != ""
			return true, None
		case "up", "down", "tab":
			a.otherOn[a.tab] = strings.TrimSpace(a.other[a.tab].Value()) != ""
			a.editing = false
		default:
			return a.other[a.tab].HandleKey(k), None
		}
	}
	act, digit := keyAct(k)
	if digit > 0 {
		if a.tab < len(a.qs) {
			if digit <= len(a.qs[a.tab].Options)+1 {
				a.cursor[a.tab] = digit - 1
				return a.activate(false)
			}
			return true, None
		}
		if a.review.pick(digit) {
			return a.do(actYes)
		}
		return true, None
	}
	return a.do(act)
}

func (a *AskQuestion) do(act act) (bool, Effect) {
	if a.tab == len(a.qs) { // review
		switch act {
		case actUp:
			a.review.move(-1)
		case actDown:
			a.review.move(1)
		case actYes:
			if a.review.selected().id == "cancel" {
				a.Deny()
				return true, Answered
			}
			return a.submit()
		case actNo:
			a.Deny()
			return true, Answered
		case actLeft, actCycleMode, actPrevField:
			a.switchTab(-1)
		case actRight, actNextField:
			a.switchTab(1)
		default:
			return false, None
		}
		return true, None
	}
	switch act {
	case actUp:
		a.cursor[a.tab] = max(0, a.cursor[a.tab]-1)
	case actDown:
		a.cursor[a.tab] = min(a.rows(a.tab)-1, a.cursor[a.tab]+1)
	case actYes:
		return a.activate(false)
	case actToggle:
		return a.activate(true)
	case actNo:
		a.Deny()
		return true, Answered
	case actLeft, actCycleMode, actPrevField:
		a.switchTab(-1)
	case actRight, actNextField:
		a.switchTab(1)
	default:
		return false, None
	}
	return true, None
}

// Action implements Model.
func (a *AskQuestion) Action(id string) (bool, Effect) {
	if a.Done() {
		return false, None
	}
	if a.editing {
		switch actionAct(id) {
		case actYes:
			return a.commitOther()
		case actNo:
			a.editing = false
			return true, None
		}
		return false, None
	}
	return a.do(actionAct(id))
}

// HandlePaste implements Model.
func (a *AskQuestion) HandlePaste(s string) (bool, Effect) {
	if !a.editing {
		return false, None
	}
	a.other[a.tab].insert(s)
	return true, None
}

func (a *AskQuestion) tabsLine(w int, st Styles) string {
	var parts []string
	for i, q := range a.qs {
		mark := "☐"
		if _, ok := a.answer(i); ok {
			mark = "☒"
		}
		label := mark + " " + SanitizeLine(q.Header)
		if i == a.tab {
			label = render(st.Selected, label)
		} else {
			label = render(st.Dim, label)
		}
		parts = append(parts, label)
	}
	submit := "✔ Submit"
	if a.tab == len(a.qs) {
		submit = render(st.Selected, submit)
	} else {
		submit = render(st.Dim, submit)
	}
	parts = append(parts, submit)
	line := "← " + strings.Join(parts, "  ") + " →"
	return line
}

// View implements Model.
func (a *AskQuestion) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	if at := attribution(a.ctx.Engine, a.ctx.Agent); at != "" {
		body = append(body, styleLines(st.Accent, wrap(at, w))...)
	}
	single := len(a.qs) == 1 && !a.qs[0].MultiSelect
	if !single {
		body = append(body, wrap(a.tabsLine(w, st), w)...)
		body = append(body, "")
	}
	title := "Question"
	if a.tab == len(a.qs) {
		title = "Review"
		body = append(body, render(st.Title, "Review your answers"))
		for i, q := range a.qs {
			ans, ok := a.answer(i)
			if !ok {
				ans = render(st.Warning, "(no answer)")
			} else {
				ans = SanitizeLine(ans)
			}
			body = append(body, wrapIndent(" • ", SanitizeLine(q.Question), w)...)
			body = append(body, wrapIndent("   → ", ans, w)...)
		}
		body = append(body, "")
		body = append(body, a.review.lines(w, st, true)...)
		body = append(body, styleLines(st.Dim, wrap("enter to confirm · ←/→ to switch questions · esc to cancel", w))...)
		return frame(title+counter(a.ctx.Index, a.ctx.Total), body, width, "permission", st)
	}

	i := a.tab
	q := a.qs[i]
	if q.Header != "" {
		title = SanitizeLine(q.Header)
	}
	body = append(body, styleLines(st.Title, wrap(Sanitize(q.Question), w))...)
	cur := a.cursor[i]
	for j, o := range q.Options {
		pointer := "  "
		if j == cur {
			pointer = "❯ "
		}
		box := ""
		if q.MultiSelect {
			box = "[ ] "
			if a.picked[i][j] {
				box = "[x] "
			}
		} else if a.picked[i][j] {
			box = "● "
		}
		lines := wrapIndent(pointer+itoa(j+1)+". "+box, SanitizeLine(o.Label), w)
		if j == cur {
			lines = styleLines(st.Selected, lines)
		}
		body = append(body, lines...)
		if o.Description != "" {
			body = append(body, styleLines(st.Dim, wrapIndent("     ", Sanitize(o.Description), w))...)
		}
	}
	// The Other row.
	n := len(q.Options)
	pointer := "  "
	if cur == n {
		pointer = "❯ "
	}
	otherOn := a.otherOn[i] || a.editing && strings.TrimSpace(a.other[i].Value()) != ""
	box := ""
	if q.MultiSelect {
		box = "[ ] "
		if otherOn {
			box = "[x] "
		}
	} else if otherOn {
		box = "● "
	}
	prefix := pointer + itoa(n+1) + ". " + box
	if a.editing || a.other[i].Value() != "" {
		lines := a.other[i].lines(w, st, a.editing, prefix)
		if cur == n {
			lines[0] = render(st.Selected, prefix) + strings.TrimPrefix(lines[0], prefix)
		}
		body = append(body, lines...)
	} else {
		l := prefix + "Other"
		if cur == n {
			l = render(st.Selected, l)
		}
		body = append(body, l)
	}
	if q.MultiSelect {
		l := "  " + "Next →"
		if a.tab == len(a.qs)-1 {
			l = "  " + "Review →"
		}
		if cur == n+1 {
			l = render(st.Selected, "❯ "+strings.TrimPrefix(l, "  "))
		}
		body = append(body, l)
	}
	if cur < n && q.Options[cur].Preview != "" {
		body = append(body, "", render(st.Dim, "Preview"))
		src := strings.TrimRight(Sanitize(string(q.Options[cur].Preview)), "\n")
		var lines []string
		if a.ctx.Markdown != nil {
			lines = a.ctx.Markdown(src, w-2)
		} else {
			lines = wrap(src, w-2)
		}
		var prev []string
		for _, l := range lines {
			prev = append(prev, "│ "+l)
		}
		body = append(body, styleLines(st.Code, truncateLines(prev, 12, st))...)
	}
	body = append(body, "")
	hint := "enter to select · ↑/↓ to move · esc to cancel"
	if q.MultiSelect {
		hint = "space to toggle · enter on Next to continue · esc to cancel"
	}
	if !single {
		hint += " · tab for next question"
	}
	body = append(body, styleLines(st.Dim, wrap(hint, w))...)
	return frame(title+counter(a.ctx.Index, a.ctx.Total), body, width, "permission", st)
}
