package dialogs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// formField is one property of an elicitation's requested_schema. The supported subset:
// string (with format, minLength, maxLength), number/integer (minimum, maximum), boolean
// and enum (string enum with optional enumNames).
type formField struct {
	name, title, description string
	typ                      string // "string" | "number" | "integer" | "boolean" | "enum"
	enum, enumNames          []string
	required                 bool
	format                   string
	minLen, maxLen           *int
	minimum, maximum         *float64

	text   textField
	on     bool
	choice int // enum index, -1 = none
	err    string
}

func (f *formField) label() string {
	l := f.title
	if l == "" {
		l = f.name
	}
	if f.required {
		l += " *"
	}
	return SanitizeLine(l)
}

// value validates the field and returns its JSON value; ok=false with f.err set when
// invalid, present=false for an empty optional field.
func (f *formField) value() (v any, present, ok bool) {
	f.err = ""
	switch f.typ {
	case "boolean":
		return f.on, true, true
	case "enum":
		if f.choice < 0 {
			if f.required {
				f.err = "Choose a value."
				return nil, false, false
			}
			return nil, false, true
		}
		return f.enum[f.choice], true, true
	}
	s := strings.TrimSpace(f.text.Value())
	if s == "" {
		if f.required {
			f.err = "Required."
			return nil, false, false
		}
		return nil, false, true
	}
	switch f.typ {
	case "number", "integer":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			f.err = "Enter a number."
			return nil, false, false
		}
		if f.typ == "integer" && n != math.Trunc(n) {
			f.err = "Enter a whole number."
			return nil, false, false
		}
		if f.minimum != nil && n < *f.minimum {
			f.err = "Must be at least " + strconv.FormatFloat(*f.minimum, 'g', -1, 64) + "."
			return nil, false, false
		}
		if f.maximum != nil && n > *f.maximum {
			f.err = "Must be at most " + strconv.FormatFloat(*f.maximum, 'g', -1, 64) + "."
			return nil, false, false
		}
		if f.typ == "integer" {
			return int64(n), true, true
		}
		return n, true, true
	}
	n := len([]rune(s))
	if f.minLen != nil && n < *f.minLen {
		f.err = fmt.Sprintf("At least %d characters.", *f.minLen)
		return nil, false, false
	}
	if f.maxLen != nil && n > *f.maxLen {
		f.err = fmt.Sprintf("At most %d characters.", *f.maxLen)
		return nil, false, false
	}
	if f.format == "email" && !strings.Contains(s, "@") {
		f.err = "Enter an email address."
		return nil, false, false
	}
	return s, true, true
}

// parseSchema reads the object schema's properties in document order.
func parseSchema(raw json.RawMessage) ([]*formField, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var top struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	names, props, err := orderedObject(top.Properties)
	if err != nil {
		return nil, err
	}
	req := map[string]bool{}
	for _, r := range top.Required {
		req[r] = true
	}
	var out []*formField
	for _, name := range names {
		var p struct {
			Type        string   `json:"type"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Enum        []any    `json:"enum"`
			EnumNames   []string `json:"enumNames"`
			OneOf       []struct {
				Const any    `json:"const"`
				Title string `json:"title"`
			} `json:"oneOf"`
			Format    string   `json:"format"`
			MinLength *int     `json:"minLength"`
			MaxLength *int     `json:"maxLength"`
			Minimum   *float64 `json:"minimum"`
			Maximum   *float64 `json:"maximum"`
			Default   any      `json:"default"`
		}
		if err := json.Unmarshal(props[name], &p); err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		f := &formField{
			name: name, title: p.Title, description: p.Description, typ: p.Type,
			required: req[name], format: p.Format, minLen: p.MinLength, maxLen: p.MaxLength,
			minimum: p.Minimum, maximum: p.Maximum, choice: -1,
		}
		for _, e := range p.Enum {
			f.enum = append(f.enum, fmt.Sprint(e))
		}
		f.enumNames = p.EnumNames
		for _, o := range p.OneOf {
			f.enum = append(f.enum, fmt.Sprint(o.Const))
			f.enumNames = append(f.enumNames, o.Title)
		}
		if len(f.enum) > 0 {
			f.typ = "enum"
		}
		switch f.typ {
		case "string", "number", "integer", "boolean", "enum":
		default:
			f.typ = "string"
		}
		switch d := p.Default.(type) {
		case bool:
			f.on = d
		case string:
			f.text.SetValue(d)
			for i, e := range f.enum {
				if e == d {
					f.choice = i
				}
			}
		case float64:
			f.text.SetValue(strconv.FormatFloat(d, 'g', -1, 64))
		}
		out = append(out, f)
	}
	return out, nil
}

// orderedObject decodes a JSON object keeping key order.
func orderedObject(raw json.RawMessage) ([]string, map[string]json.RawMessage, error) {
	vals := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, vals, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("properties: not an object")
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		k, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		if _, dup := vals[k]; !dup {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	return keys, vals, nil
}

// Elicitation is the view-model of an MCP `elicitation` request: a form built from the
// requested schema, or a URL to open (mode "url").
type Elicitation struct {
	req    ElicitationRequest
	ctx    PermissionContext
	fields []*formField
	row    int // 0..len(fields)-1 fields, then buttons
	btns   optionList
	result *ElicitationResult
	err    error
}

// NewElicitation builds the dialog. A schema mantle can't parse still gets a dialog
// offering decline/cancel only.
func NewElicitation(req ElicitationRequest, ctx PermissionContext) *Elicitation {
	e := &Elicitation{req: req, ctx: ctx}
	if req.Mode == "url" {
		e.btns = optionList{items: []option{{id: "open", label: "Open the link"}, {id: "decline", label: "Decline"}, {id: "cancel", label: "Cancel"}}}
		return e
	}
	e.fields, e.err = parseSchema(req.RequestedSchema)
	items := []option{{id: "accept", label: "Submit"}, {id: "decline", label: "Decline"}, {id: "cancel", label: "Cancel"}}
	if e.err != nil {
		items = items[1:]
	}
	e.btns = optionList{items: items}
	return e
}

// Done reports whether the dialog has been answered.
func (e *Elicitation) Done() bool { return e.result != nil }

// SetPosition implements Positioner.
func (e *Elicitation) SetPosition(index, total int) { e.ctx.Index, e.ctx.Total = index, total }

// Response is the answer to send; nil until Done.
func (e *Elicitation) Response() *ElicitationResult { return e.result }

// URL is the link to open in URL mode.
func (e *Elicitation) URL() string { return e.req.URL }

// Cancel answers with cancel (the user dismissed the dialog).
func (e *Elicitation) Cancel() *ElicitationResult {
	if e.result == nil {
		e.result = &ElicitationResult{Action: "cancel"}
	}
	return e.result
}

func (e *Elicitation) onButtons() bool { return e.row >= len(e.fields) }

func (e *Elicitation) field() *formField {
	if e.onButtons() {
		return nil
	}
	return e.fields[e.row]
}

func (e *Elicitation) move(d int) {
	if e.onButtons() && (d < 0 && e.btns.cursor > 0 || d > 0 && e.btns.cursor < len(e.btns.items)-1) {
		e.btns.move(d)
		return
	}
	e.row = min(max(e.row+d, 0), len(e.fields))
	if e.onButtons() && d < 0 {
		e.btns.cursor = len(e.btns.items) - 1
	} else if e.onButtons() {
		e.btns.cursor = 0
	}
}

func (e *Elicitation) press() (bool, Effect) {
	switch e.btns.selected().id {
	case "open":
		e.result = &ElicitationResult{Action: "accept"}
		return true, OpenURL
	case "decline":
		e.result = &ElicitationResult{Action: "decline"}
		return true, Answered
	case "cancel":
		e.Cancel()
		return true, Answered
	}
	content := map[string]any{}
	firstBad := -1
	for i, f := range e.fields {
		v, present, ok := f.value()
		if !ok {
			if firstBad < 0 {
				firstBad = i
			}
			continue
		}
		if present {
			content[f.name] = v
		}
	}
	if firstBad >= 0 {
		e.row = firstBad
		return true, None
	}
	e.result = &ElicitationResult{Action: "accept", Content: content}
	return true, Answered
}

// HandleKey implements Model.
func (e *Elicitation) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if e.Done() {
		return false, None
	}
	switch k.Keystroke() {
	case "esc":
		e.Cancel()
		return true, Answered
	case "up", "shift+tab":
		e.move(-1)
		return true, None
	case "down", "tab":
		e.move(1)
		return true, None
	case "enter":
		if e.onButtons() {
			return e.press()
		}
		e.move(1)
		return true, None
	}
	f := e.field()
	if f == nil {
		if a, digit := keyAct(k); digit > 0 {
			if e.btns.pick(digit) {
				return e.press()
			}
			return true, None
		} else if a == actLeft {
			e.btns.move(-1)
			return true, None
		} else if a == actRight {
			e.btns.move(1)
			return true, None
		}
		return false, None
	}
	switch f.typ {
	case "boolean":
		switch k.Keystroke() {
		case "space", "left", "right":
			f.on = !f.on
			return true, None
		}
		return false, None
	case "enum":
		switch k.Keystroke() {
		case "space", "right":
			f.choice = (f.choice + 1) % len(f.enum)
			return true, None
		case "left":
			if f.choice <= 0 {
				f.choice = len(f.enum) - 1
			} else {
				f.choice--
			}
			return true, None
		}
		return false, None
	}
	if f.text.HandleKey(k) {
		f.err = ""
		return true, None
	}
	return false, None
}

// Action implements Model.
func (e *Elicitation) Action(id string) (bool, Effect) {
	if e.Done() {
		return false, None
	}
	switch actionAct(id) {
	case actYes:
		return e.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	case actNo:
		return e.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	case actUp, actPrevField:
		e.move(-1)
	case actDown, actNextField:
		e.move(1)
	case actToggle:
		return e.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	default:
		return false, None
	}
	return true, None
}

// HandlePaste implements Model.
func (e *Elicitation) HandlePaste(s string) (bool, Effect) {
	f := e.field()
	if f == nil || f.typ == "boolean" || f.typ == "enum" {
		return false, None
	}
	f.text.insert(s)
	return true, None
}

// View implements Model.
func (e *Elicitation) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	if a := attribution(e.ctx.Engine, e.ctx.Agent); a != "" {
		body = append(body, styleLines(st.Accent, wrap(a, w))...)
	}
	server := SanitizeLine(e.req.McpServerName)
	body = append(body, styleLines(st.Dim, wrap("MCP server "+server+" is asking:", w))...)
	if e.req.Message != "" {
		body = append(body, styleLines(st.Text, wrap(Sanitize(e.req.Message), w))...)
	}
	if e.req.Mode == "url" {
		body = append(body, "")
		body = append(body, styleLines(st.Code, wrapIndent("  ", SanitizeLine(e.req.URL), w))...)
		body = append(body, "")
		body = append(body, e.btns.lines(w, st, true)...)
		body = append(body, render(st.Dim, "esc to cancel"))
		return frame(e.title(), body, width, "permission", st)
	}
	if e.err != nil {
		body = append(body, styleLines(st.Error, wrap("This form can't be shown: "+SanitizeLine(e.err.Error()), w))...)
	}
	for i, f := range e.fields {
		body = append(body, "")
		focused := i == e.row
		label := f.label()
		if focused {
			body = append(body, render(st.Selected, "❯ "+label))
		} else {
			body = append(body, "  "+label)
		}
		if f.description != "" {
			body = append(body, styleLines(st.Dim, wrapIndent("  ", Sanitize(f.description), w))...)
		}
		switch f.typ {
		case "boolean":
			box := "[ ] "
			if f.on {
				box = "[x] "
			}
			body = append(body, "  "+box+map[bool]string{true: "Yes", false: "No"}[f.on])
		case "enum":
			var parts []string
			for j, v := range f.enum {
				name := v
				if j < len(f.enumNames) && f.enumNames[j] != "" {
					name = f.enumNames[j]
				}
				name = SanitizeLine(name)
				if j == f.choice {
					name = render(st.Selected, "("+name+")")
				}
				parts = append(parts, name)
			}
			body = append(body, wrapIndent("  ", strings.Join(parts, "  "), w)...)
		default:
			body = append(body, f.text.lines(w, st, focused, "  > ")...)
		}
		if f.err != "" {
			body = append(body, render(st.Error, "  "+f.err))
		}
	}
	body = append(body, "")
	body = append(body, e.btns.lines(w, st, e.onButtons())...)
	body = append(body, styleLines(st.Dim, wrap("tab/↑↓ to move · space to toggle · enter to continue · esc to cancel", w))...)
	return frame(e.title(), body, width, "permission", st)
}

func (e *Elicitation) title() string {
	t := "Input requested"
	if e.req.Title != "" {
		t = SanitizeLine(e.req.Title)
	}
	return t + counter(e.ctx.Index, e.ctx.Total)
}
