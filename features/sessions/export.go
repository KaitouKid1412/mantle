package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/clipcmd"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const (
	DialogExport = "dialog.export"
	DialogCopy   = "dialog.copy"

	tagExport = "sessions.export"
	tagCopy   = "sessions.copy"
)

func (f *feature) registerExport(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("export"), Name: "export", Source: ext.SourceBuiltin,
		Description: "Export the conversation to a file or the clipboard",
		ArgHint:     "[filename]",
		Run:         f.runExport,
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("copy"), Name: "copy", Source: ext.SourceBuiltin,
		Description: "Copy Claude's last response (or the Nth latest) to the clipboard",
		ArgHint:     "[N]",
		Run:         f.runCopy,
	})
	r.AddDialog(DialogExport, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		return f.exportDialog(ctx), nil
	})
	r.AddDialog(DialogCopy, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		resp, _ := args.(string)
		return f.copyDialog(ctx, resp), nil
	})
	ext.Subscribe(r, "sessions.copied", func(ctx ext.Ctx, m clipcmd.CopiedMsg) tea.Cmd {
		if m.Tag != tagExport && m.Tag != tagCopy {
			return nil
		}
		level := ext.NoticeSuccess
		if m.Err != nil {
			level = ext.NoticeError
		}
		return notice(ctx, "copied", m.Notice(), level)
	})
}

// ---- /export ----

func (f *feature) runExport(ctx ext.Ctx, args string) tea.Cmd {
	text := transcriptText(ctx.Transcript())
	if strings.TrimSpace(text) == "" {
		return notice(ctx, "export", "Nothing to export yet", ext.NoticeInfo)
	}
	if name := strings.TrimSpace(args); name != "" {
		return writeExport(ctx, f.exportPath(ctx, name), text)
	}
	return ctx.OpenDialog(DialogExport, nil)
}

func (f *feature) exportDialog(ctx ext.Ctx) ext.Dialog {
	text := transcriptText(ctx.Transcript())
	path := f.exportPath(ctx, "")
	return &choiceDialog{
		id: DialogExport, title: "Export conversation",
		choices: []choice{
			{label: "Copy to clipboard", run: func(ctx ext.Ctx) tea.Cmd { return clipcmd.Copy(tagExport, text) }},
			{label: "Save to file", detail: filepath.Base(path), run: func(ctx ext.Ctx) tea.Cmd { return writeExport(ctx, path, text) }},
		},
	}
}

// exportPath resolves an export file name against the cwd; with no name it makes one
// from the date and the conversation title. A name without an extension gets .txt.
func (f *feature) exportPath(ctx ext.Ctx, name string) string {
	if name == "" {
		stamp := ctx.Clock().Now().Format("2006-01-02-150405")
		name = stamp
		if t := slugify(ctx.Session().Title); t != "" {
			name += "-" + t
		}
	}
	if filepath.Ext(name) == "" {
		name += ".txt"
	}
	return expandPath(name, f.cwd(ctx))
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

func writeExport(ctx ext.Ctx, path, text string) tea.Cmd {
	return func() tea.Msg {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return notifyMsg{key: "export", text: "Export failed: " + err.Error(), level: ext.NoticeError}
		}
		return notifyMsg{key: "export", text: "Conversation exported to " + path, level: ext.NoticeSuccess}
	}
}

// transcriptText renders the transcript as plain text: prompts, responses, tool calls
// with the start of their results, and session events.
func transcriptText(t ext.Transcript) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	for _, it := range t.Items() {
		if it.ParentID != "" {
			continue
		}
		block := itemText(it)
		if block == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block)
		b.WriteString("\n")
	}
	return b.String()
}

const exportResultLines = 20

func itemText(it *ext.Item) string {
	switch d := it.Data.(type) {
	case *proto.User:
		text := strings.TrimSpace(d.Message.Content.PlainText())
		if it.Key == ext.KeyUserBash {
			return "! " + strings.TrimSpace(bashInput(text))
		}
		if text == "" {
			return ""
		}
		return prefixLines(text, "> ", "  ")
	case *proto.ContentBlock:
		if it.Key == ext.KeyAssistantText {
			return prefixLines(strings.TrimSpace(d.Text), "⏺ ", "  ")
		}
		return ""
	case *proto.ToolUse:
		head := "⏺ " + d.Name
		if s := toolSummary(d); s != "" {
			head += "(" + s + ")"
		}
		if it.Result == nil {
			return head
		}
		out := strings.TrimRight(it.Result.Content.PlainText(), "\n")
		if out == "" {
			return head
		}
		lines := strings.Split(out, "\n")
		more := 0
		if len(lines) > exportResultLines {
			more = len(lines) - exportResultLines
			lines = lines[:exportResultLines]
		}
		res := prefixLines(strings.Join(lines, "\n"), "  ⎿ ", "    ")
		if more > 0 {
			res += "\n    … +" + strconv.Itoa(more) + " lines"
		}
		return head + "\n" + res
	case *proto.CompactBoundary:
		return "─── conversation compacted ───"
	case *proto.LocalCommandOutput:
		return prefixLines(strings.TrimSpace(d.Content), "  ⎿ ", "    ")
	case *proto.Informational:
		return "※ " + oneLine(d.Content)
	case *proto.Assistant:
		var parts []string
		for _, c := range d.Message.Content {
			if c.Type == proto.BlockText {
				parts = append(parts, c.Text)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if d.Error != "" {
			return "⚠ " + oneLine(text)
		}
		return prefixLines(text, "  ⎿ ", "    ")
	}
	return ""
}

var bashInputRE = regexp.MustCompile(`(?s)<bash-input>(.*?)</bash-input>`)

func bashInput(s string) string {
	if m := bashInputRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// toolSummary is the argument that identifies a tool call (command, path, pattern…).
func toolSummary(t *proto.ToolUse) string {
	var in map[string]any
	if len(t.Input) == 0 || json.Unmarshal(t.Input, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "notebook_path", "pattern", "url", "query", "description", "prompt", "path"} {
		if v, ok := in[k].(string); ok && v != "" {
			return truncateRunes(oneLine(v), 100)
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else if lines[i] != "" {
			lines[i] = rest + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// ---- /copy ----

// responses returns the assistant's responses (the text of each turn), oldest first.
func responses(t ext.Transcript) []string {
	if t == nil {
		return nil
	}
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n\n"))
			cur = nil
		}
	}
	for _, it := range t.Items() {
		if it.ParentID != "" {
			continue
		}
		switch it.Key {
		case ext.KeyUserPrompt, ext.KeyUserBash:
			flush()
		case ext.KeyAssistantText:
			if b, ok := it.Data.(*proto.ContentBlock); ok && strings.TrimSpace(b.Text) != "" {
				cur = append(cur, strings.TrimSpace(b.Text))
			}
		}
	}
	flush()
	return out
}

func (f *feature) runCopy(ctx ext.Ctx, args string) tea.Cmd {
	n := 1
	if a := strings.TrimSpace(args); a != "" {
		v, err := strconv.Atoi(a)
		if err != nil || v < 1 {
			return notice(ctx, "copy", "Usage: /copy [N] (1 = the latest response)", ext.NoticeInfo)
		}
		n = v
	}
	rs := responses(ctx.Transcript())
	if len(rs) == 0 {
		return notice(ctx, "copy", "No response to copy yet", ext.NoticeInfo)
	}
	if n > len(rs) {
		return notice(ctx, "copy", "Only "+plural(len(rs), "response", "responses")+" so far", ext.NoticeInfo)
	}
	resp := rs[len(rs)-n]
	if len(codeBlocks(resp)) == 0 || ext.ClaudeBool(ctx.Settings(), "copyFullResponse", false) {
		return clipcmd.Copy(tagCopy, resp)
	}
	return ctx.OpenDialog(DialogCopy, resp)
}

// codeBlock is a fenced code block of a response.
type codeBlock struct {
	lang, code string
}

var fenceRE = regexp.MustCompile("(?ms)^[ \t]*(`{3,}|~{3,})[ \t]*([\\w+#.-]*)[^\n]*\n(.*?)^[ \t]*(?:`{3,}|~{3,})[ \t]*$")

func codeBlocks(s string) []codeBlock {
	var out []codeBlock
	for _, m := range fenceRE.FindAllStringSubmatch(s, -1) {
		out = append(out, codeBlock{lang: m[2], code: strings.TrimRight(m[3], "\n")})
	}
	return out
}

func (f *feature) copyDialog(ctx ext.Ctx, resp string) ext.Dialog {
	blocks := codeBlocks(resp)
	choices := []choice{{
		label: "Full response", detail: plural(len(strings.Split(resp, "\n")), "line", "lines"),
		run: func(ctx ext.Ctx) tea.Cmd { return clipcmd.Copy(tagCopy, resp) },
	}}
	contents := []string{resp}
	names := []string{"response.md"}
	for i, b := range blocks {
		code := b.code
		label := "Code block " + strconv.Itoa(i+1)
		if b.lang != "" {
			label += " (" + b.lang + ")"
		}
		choices = append(choices, choice{
			label: label, detail: truncateRunes(oneLine(firstLine(code)), 50),
			run: func(ctx ext.Ctx) tea.Cmd { return clipcmd.Copy(tagCopy, code) },
		})
		contents = append(contents, code+"\n")
		names = append(names, "snippet"+langExt(b.lang))
	}
	cwd := f.cwd(ctx)
	return &choiceDialog{
		id: DialogCopy, title: "Copy to clipboard", choices: choices,
		hint: "enter copy · w write to a file · esc cancel",
		keys: map[string]func(ext.Ctx, choice, int) tea.Cmd{
			"w": func(ctx ext.Ctx, _ choice, i int) tea.Cmd {
				return writeFile(uniquePath(filepath.Join(cwd, names[i])), contents[i])
			},
		},
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func writeFile(path, content string) tea.Cmd {
	return func() tea.Msg {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return notifyMsg{key: "copy", text: "Could not write " + path + ": " + err.Error(), level: ext.NoticeError}
		}
		return notifyMsg{key: "copy", text: "Wrote " + path, level: ext.NoticeSuccess}
	}
}

// uniquePath adds -1, -2… before the extension until the path is free.
func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		c := base + "-" + strconv.Itoa(i) + ext
		if _, err := os.Stat(c); err != nil {
			return c
		}
	}
}

func langExt(lang string) string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return ".go"
	case "python", "py":
		return ".py"
	case "javascript", "js":
		return ".js"
	case "typescript", "ts":
		return ".ts"
	case "tsx":
		return ".tsx"
	case "jsx":
		return ".jsx"
	case "bash", "sh", "shell", "zsh":
		return ".sh"
	case "rust", "rs":
		return ".rs"
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "html":
		return ".html"
	case "css":
		return ".css"
	case "sql":
		return ".sql"
	case "java":
		return ".java"
	case "c":
		return ".c"
	case "cpp", "c++":
		return ".cpp"
	case "ruby", "rb":
		return ".rb"
	case "markdown", "md":
		return ".md"
	case "diff", "patch":
		return ".diff"
	}
	return ".txt"
}
