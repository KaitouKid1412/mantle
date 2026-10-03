package claudecli

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// MCPState is a server's health as reported by "claude mcp list/get". The CLI
// prints text (no --json in 2.1.288), so the state is classified by keyword and
// the original text is kept in Status.
type MCPState int

const (
	MCPUnknown       MCPState = iota
	MCPConnected              // healthy
	MCPDegraded               // connected, but listing tools failed
	MCPNeedsAuth              // needs OAuth: claude mcp login <name>
	MCPFailed                 // could not connect
	MCPPending                // unapproved .mcp.json server, not connected to
	MCPRejected               // rejected .mcp.json server
	MCPDisabled               // disabled for this project
	MCPNotConfigured          // configuration incomplete
)

var mcpStateNames = [...]string{"unknown", "connected", "degraded", "needs-auth", "failed", "pending",
	"rejected", "disabled", "not-configured"}

func (s MCPState) String() string {
	if s < 0 || int(s) >= len(mcpStateNames) {
		return "unknown"
	}
	return mcpStateNames[s]
}

// ClassifyMCPStatus maps a status text to an MCPState. Order matters: "Connected ·
// tools fetch failed" is degraded, not connected or failed.
func ClassifyMCPStatus(status string) MCPState {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "auth"):
		return MCPNeedsAuth
	case strings.Contains(s, "pending"):
		return MCPPending
	case strings.Contains(s, "rejected"):
		return MCPRejected
	case strings.Contains(s, "disabled"):
		return MCPDisabled
	case strings.Contains(s, "not configured"):
		return MCPNotConfigured
	case strings.Contains(s, "connected") && strings.Contains(s, "fail"):
		return MCPDegraded
	case strings.Contains(s, "fail"), strings.Contains(s, "error"):
		return MCPFailed
	case strings.Contains(s, "connected"):
		return MCPConnected
	}
	return MCPUnknown
}

// MCPServer is one row of "claude mcp list".
type MCPServer struct {
	Name   string
	Target string // command line or URL
	Status string // status text without the leading glyph
	State  MCPState
	Issue  string // indented detail lines under the row, if any
}

// ParseMCPList parses "claude mcp list" output. Lines it cannot parse are
// skipped, so header and footer text changes don't break it.
func ParseMCPList(out []byte) []MCPServer {
	var servers []MCPServer
	for _, raw := range strings.Split(ansi.Strip(string(out)), "\n") {
		line := strings.TrimRight(raw, " \r\t")
		if line == "" {
			continue
		}
		indented := line[0] == ' ' || line[0] == '\t'
		if indented {
			if n := len(servers); n > 0 {
				servers[n-1].Issue = joinLines(servers[n-1].Issue, strings.TrimSpace(line))
			}
			continue
		}
		name, rest, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, " - ")
		if i < 0 {
			continue
		}
		target, status := rest[:i], stripGlyph(rest[i+3:])
		// "- Not configured" uses "-" as its glyph: "name: url - - Not configured".
		target = strings.TrimSuffix(strings.TrimSpace(target), " -")
		servers = append(servers, MCPServer{
			Name:   name,
			Target: target,
			Status: status,
			State:  ClassifyMCPStatus(status),
		})
	}
	return servers
}

// MCPField is one "Key: value" line of "claude mcp get". Keys printed with no
// value and indented lines below (Environment, Headers) carry those lines in List.
type MCPField struct {
	Key   string
	Value string
	List  []string
}

// MCPServerDetail is the parsed output of "claude mcp get <name>".
type MCPServerDetail struct {
	Name   string
	Fields []MCPField
	Notes  []string // unindented lines after the fields, such as the remove hint
}

// Field returns the value of the first field whose key matches (case-insensitive).
func (d MCPServerDetail) Field(key string) (MCPField, bool) {
	for _, f := range d.Fields {
		if strings.EqualFold(f.Key, key) {
			return f, true
		}
	}
	return MCPField{}, false
}

func (d MCPServerDetail) value(key string) string {
	f, _ := d.Field(key)
	return f.Value
}

// Scope is the scope text, e.g. "Local config (private to you in this project)".
func (d MCPServerDetail) Scope() string { return d.value("Scope") }

// Status is the status text without its glyph.
func (d MCPServerDetail) Status() string { return stripGlyph(d.value("Status")) }

// State classifies Status.
func (d MCPServerDetail) State() MCPState { return ClassifyMCPStatus(d.Status()) }

// Type is the transport (stdio, sse, http, …).
func (d MCPServerDetail) Type() string { return d.value("Type") }

// ParseMCPGet parses "claude mcp get <name>" output.
func ParseMCPGet(out []byte) (MCPServerDetail, error) {
	var d MCPServerDetail
	var cur *MCPField
	baseIndent := -1
	for _, raw := range strings.Split(ansi.Strip(string(out)), "\n") {
		line := strings.TrimRight(raw, " \r\t")
		if strings.TrimSpace(line) == "" {
			cur = nil
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		text := strings.TrimSpace(line)
		switch {
		case d.Name == "" && indent == 0 && strings.HasSuffix(text, ":"):
			d.Name = strings.TrimSuffix(text, ":")
		case d.Name == "":
			// preamble such as a health-check banner
		case indent == 0:
			d.Notes = append(d.Notes, text)
			cur = nil
		case baseIndent < 0 || indent <= baseIndent:
			baseIndent = indent
			key, value, _ := strings.Cut(text, ":")
			d.Fields = append(d.Fields, MCPField{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value)})
			cur = &d.Fields[len(d.Fields)-1]
		case cur != nil:
			cur.List = append(cur.List, text)
		}
	}
	if d.Name == "" {
		return d, errors.New("claude mcp get: unrecognised output")
	}
	return d, nil
}

// MCPList runs "claude mcp list". It health-checks every server, so it can take
// tens of seconds; prefer the engine's mcp_status while an engine is running.
func (r *Runner) MCPList(ctx context.Context) ([]MCPServer, error) {
	out, _, err := r.Run(ctx, MCPList)
	if err != nil {
		return nil, err
	}
	return ParseMCPList(out), nil
}

// MCPGet runs "claude mcp get <name>".
func (r *Runner) MCPGet(ctx context.Context, name string) (MCPServerDetail, error) {
	out, _, err := r.Run(ctx, MCPGet, "--", name)
	if err != nil {
		return MCPServerDetail{}, err
	}
	return ParseMCPGet(out)
}

// MCPAddOptions describes "claude mcp add".
type MCPAddOptions struct {
	Name         string
	CommandOrURL string
	Args         []string // stdio command arguments
	Scope        string   // local (default), user or project
	Transport    string   // stdio (default), sse or http
	Env          []string // KEY=VALUE
	Headers      []string // "Name: value"
	ClientID     string
	ClientSecret string // passed through MCP_CLIENT_SECRET, never argv
	CallbackPort int
}

// MCPAdd runs "claude mcp add". Operands follow "--", so command arguments that
// start with "-" reach the server command unchanged.
func (r *Runner) MCPAdd(ctx context.Context, o MCPAddOptions) error {
	var args []string
	args = appendValue(args, "--scope", o.Scope)
	args = appendValue(args, "--transport", o.Transport)
	for _, e := range o.Env {
		args = append(args, "--env="+e)
	}
	for _, h := range o.Headers {
		args = append(args, "--header="+h)
	}
	args = appendValue(args, "--client-id", o.ClientID)
	var env []string
	if o.ClientSecret != "" {
		args = append(args, "--client-secret")
		env = append(env, "MCP_CLIENT_SECRET="+o.ClientSecret)
	}
	if o.CallbackPort > 0 {
		args = append(args, "--callback-port="+strconv.Itoa(o.CallbackPort))
	}
	args = append(args, "--", o.Name, o.CommandOrURL)
	args = append(args, o.Args...)
	_, err := r.Do(ctx, Call{Sub: MCPAdd, Args: args, Env: env})
	return err
}

// MCPAddJSON runs "claude mcp add-json <name> <json>".
func (r *Runner) MCPAddJSON(ctx context.Context, name, json, scope, clientSecret string) error {
	args := appendValue(nil, "--scope", scope)
	var env []string
	if clientSecret != "" {
		args = append(args, "--client-secret")
		env = append(env, "MCP_CLIENT_SECRET="+clientSecret)
	}
	args = append(args, "--", name, json)
	_, err := r.Do(ctx, Call{Sub: MCPAddJSON, Args: args, Env: env})
	return err
}

// MCPRemove runs "claude mcp remove [--scope] <name>". An empty scope removes the
// server from whichever scope has it.
func (r *Runner) MCPRemove(ctx context.Context, name, scope string) error {
	args := append(appendValue(nil, "--scope", scope), "--", name)
	_, _, err := r.Run(ctx, MCPRemove, args...)
	return err
}

// MCPLogout runs "claude mcp logout <name>" (clears stored OAuth credentials).
func (r *Runner) MCPLogout(ctx context.Context, name string) error {
	_, _, err := r.Run(ctx, MCPLogout, "--", name)
	return err
}

// MCPLoginCmd returns "claude mcp login [--no-browser] <name>" for
// tea.ExecProcess. Reconnect the server through the engine afterwards.
func (r *Runner) MCPLoginCmd(name string, noBrowser bool) (*exec.Cmd, error) {
	var args []string
	if noBrowser {
		args = append(args, "--no-browser")
	}
	return r.Interactive(MCPLogin, append(args, "--", name)...)
}

func appendValue(args []string, flag, value string) []string {
	if value == "" {
		return args
	}
	return append(args, flag+"="+value)
}

// stripGlyph removes a leading status symbol ("✔ ", "! ", "⏸ ", "- ", …).
func stripGlyph(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i > 0 && i <= 4 {
		head := s[:i]
		letters := false
		for _, r := range head {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				letters = true
				break
			}
		}
		if !letters {
			return strings.TrimSpace(s[i+1:])
		}
	}
	return s
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}
