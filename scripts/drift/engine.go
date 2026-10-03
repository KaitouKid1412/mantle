package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// EngineReport is what a throwaway engine session reports about itself.
type EngineReport struct {
	Commands     []Item // initialize.commands: name, aliases
	Models       []Item // initialize.models[].value
	OutputStyles []Item // initialize.available_output_styles
	Tools        []Item // system/init.tools
}

// isolatedEnv returns an environment for a zero-token engine run: a fresh config dir,
// a dummy API key, and the API pointed at a closed local port, so a model call fails
// at once without reaching any provider. Credentials and provider switches are dropped.
func isolatedEnv(base []string, configDir, apiURL string) []string {
	drop := func(k string) bool {
		return k == "CLAUDECODE" || k == "CLAUDE_CONFIG_DIR" || k == "CLAUDE_CODE_OAUTH_TOKEN" ||
			k == "AWS_BEARER_TOKEN_BEDROCK" || strings.HasPrefix(k, "ANTHROPIC_") ||
			strings.HasPrefix(k, "CLAUDE_CODE_USE_") || k == "CLAUDE_CODE_SIMPLE" || k == "CLAUDE_CODE_SAFE_MODE"
	}
	var env []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop(k) {
			env = append(env, kv)
		}
	}
	return append(env,
		"CLAUDE_CONFIG_DIR="+configDir,
		"ANTHROPIC_API_KEY=sk-ant-mantle-drift-offline",
		"ANTHROPIC_BASE_URL="+apiURL,
		"CLAUDE_CODE_MAX_RETRIES=0",
		"DISABLE_TELEMETRY=1",
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)
}

// closedURL returns a local URL nothing listens on.
func closedURL() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr, nil
}

// engineSession starts claude in stream-json mode with an isolated config and an empty
// working directory (so only built-in commands and tools show), asks initialize, sends
// one prompt to get system/init (the model call fails locally: no tokens), then ends the
// session.
func (c *Collector) engineSession(ctx context.Context) (EngineReport, error) {
	var rep EngineReport
	if c.Claude == "" {
		return rep, errors.New("claude not found")
	}
	tmp, err := os.MkdirTemp("", "mantle-drift-*")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(tmp)
	cfg, cwd := filepath.Join(tmp, "config"), filepath.Join(tmp, "cwd")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		return rep, err
	}
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return rep, err
	}
	api, err := closedURL()
	if err != nil {
		return rep, err
	}

	timeout := c.EngineTimeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Claude,
		"--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--no-session-persistence")
	cmd.Dir = cwd
	cmd.Env = isolatedEnv(os.Environ(), cfg, api)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return rep, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return rep, err
	}
	if err := cmd.Start(); err != nil {
		return rep, err
	}
	defer func() {
		stdin.Close()
		_ = cmd.Wait()
	}()

	send := func(v any) error {
		b, _ := json.Marshal(v)
		_, err := stdin.Write(append(b, '\n'))
		return err
	}
	r := bufio.NewReaderSize(stdout, 1<<20)
	next := func() (map[string]any, error) {
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				var m map[string]any
				if json.Unmarshal(line, &m) == nil {
					return m, nil
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("engine session timed out after %s", timeout)
				}
				return nil, fmt.Errorf("engine output ended: %w", err)
			}
		}
	}

	if err := send(map[string]any{"type": "control_request", "request_id": "drift_init",
		"request": map[string]any{"subtype": "initialize"}}); err != nil {
		return rep, err
	}
	for {
		m, err := next()
		if err != nil {
			return rep, err
		}
		if m["type"] != "control_response" {
			continue
		}
		resp, _ := m["response"].(map[string]any)
		if resp["request_id"] != "drift_init" {
			continue
		}
		if resp["subtype"] != "success" {
			return rep, fmt.Errorf("initialize failed: %v", resp["error"])
		}
		body, _ := resp["response"].(map[string]any)
		rep.Commands, rep.Models, rep.OutputStyles = parseInitialize(body)
		break
	}

	if err := send(map[string]any{"type": "user", "parent_tool_use_id": nil, "session_id": "",
		"message": map[string]any{"role": "user", "content": "drift"}}); err != nil {
		return rep, err
	}
	for {
		m, err := next()
		if err != nil {
			return rep, err
		}
		if m["type"] == "system" && m["subtype"] == "init" {
			for _, t := range strings2(m["tools"]) {
				rep.Tools = append(rep.Tools, Item{Name: t})
			}
			break
		}
	}
	_ = send(map[string]any{"type": "control_request", "request_id": "drift_end",
		"request": map[string]any{"subtype": "end_session", "reason": "drift"}})
	return rep, nil
}

func parseInitialize(body map[string]any) (commands, models, styles []Item) {
	if cs, ok := body["commands"].([]any); ok {
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			name, _ := cm["name"].(string)
			if name == "" {
				continue
			}
			it := Item{Name: strings.TrimPrefix(name, "/")}
			if al := strings2(cm["aliases"]); len(al) > 0 {
				it.Attrs = map[string]string{"aliases": strings.Join(al, ",")}
			}
			commands = append(commands, it)
		}
	}
	if ms, ok := body["models"].([]any); ok {
		for _, m := range ms {
			mm, _ := m.(map[string]any)
			if v, _ := mm["value"].(string); v != "" {
				models = append(models, Item{Name: v})
			}
		}
	}
	for _, s := range strings2(body["available_output_styles"]) {
		styles = append(styles, Item{Name: s})
	}
	return commands, models, styles
}

// strings2 reads a JSON array of strings.
func strings2(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// collectEngine runs the engine session and returns its lists. With Offline or
// NoEngine set, or when the session fails, the lists are unavailable.
func (c *Collector) collectEngine(ctx context.Context) []List {
	source := "zero-token engine session (initialize, system/init)"
	lists := []List{
		{Kind: KindSlash, Source: source},
		{Kind: KindTool, Source: source},
		{Kind: KindOutputStyle, Source: source},
		{Kind: KindModel, Source: source},
	}
	fail := func(msg string) []List {
		for i := range lists {
			lists[i].Error = msg
		}
		return lists
	}
	if c.NoEngine {
		return fail("skipped (-no-engine)")
	}
	rep, err := c.engineSession(ctx)
	if err != nil {
		return fail(err.Error())
	}
	lists[0].Items, lists[1].Items, lists[2].Items, lists[3].Items = rep.Commands, rep.Tools, rep.OutputStyles, rep.Models
	return lists
}
