package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

// SettingsSchemaURL is the public JSON schema for Claude Code's settings files. The
// binary doesn't embed one.
const SettingsSchemaURL = "https://www.schemastore.org/claude-code-settings.json"

// Collector gathers a Snapshot from the installed claude.
type Collector struct {
	Claude  string // claude binary; resolved like the engine when empty
	Binary  string // file to scan for keybindings; Claude's symlink target when empty
	Cache   string // directory for the settings schema copy
	Offline bool   // use the cached settings schema only
	HTTP    *http.Client
	Timeout time.Duration // per claude call
	Log     io.Writer
}

// Collect runs every collector. A collector that fails yields a List with Error set;
// the run never stops early.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	var s Snapshot
	if c.Claude == "" {
		bin, err := cli.ResolveClaude()
		if err != nil {
			c.logf("claude: %v", err)
		}
		c.Claude = bin
	}
	if c.Claude != "" {
		if out, err := c.claudeInfo(ctx, "--version"); err == nil {
			s.ClaudeVersion = regexp.MustCompile(`\d+\.\d+\.\d+\S*`).FindString(string(out))
		} else {
			c.logf("claude --version: %v", err)
		}
	}

	flags, subs := c.collectHelp(ctx)
	s.Put(flags)
	s.Put(subs)
	actions, contexts, candidates := c.collectBinary(subs)
	s.Put(actions)
	s.Put(contexts)
	s.Put(candidates)
	s.Put(c.collectSettings(ctx))
	for _, kind := range []string{KindSlash, KindTool, KindOutputStyle, KindModel, KindProtocol} {
		s.Put(List{Kind: kind, Source: "engine session", Error: "not collected yet: needs a zero-token engine session (plan 11, B3)"})
	}
	return s
}

func (c *Collector) logf(format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, "drift: "+format+"\n", args...)
	}
}

// claudeInfo runs claude with --help or --version only, optionally after one known
// subcommand. argv elements are passed separately, stdin is /dev/null, and the run has
// a timeout: an unrecognised word would otherwise become a model prompt.
func (c *Collector) claudeInfo(ctx context.Context, args ...string) ([]byte, error) {
	if err := safeInfoArgs(args); err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Claude, args...)
	cmd.Stdin = nil
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "CLAUDECODE=") })
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// safeInfoArgs allows exactly: --version, --help, or <known subcommand> --help.
func safeInfoArgs(args []string) error {
	switch {
	case len(args) == 1 && (args[0] == "--version" || args[0] == "--help"):
		return nil
	case len(args) == 2 && args[1] == "--help":
		s := cli.LookupSubcommand(args[0])
		if s == nil || s.Name != args[0] {
			return fmt.Errorf("refusing to run claude %q: not a known subcommand", args[0])
		}
		if s.NoHelpProbe {
			return fmt.Errorf("refusing to run claude %s --help: it doesn't print help", args[0])
		}
		return nil
	}
	return fmt.Errorf("refusing to run claude %q", args)
}

// collectHelp parses `claude --help` and `claude <sub> --help` for every known
// subcommand that prints help.
func (c *Collector) collectHelp(ctx context.Context) (flags, subs List) {
	flags = List{Kind: KindFlag, Source: "claude --help, claude <subcommand> --help"}
	subs = List{Kind: KindSubcommand, Source: "claude --help, claude <subcommand> --help"}
	if c.Claude == "" {
		flags.Error, subs.Error = "claude not found", "claude not found"
		return flags, subs
	}
	root, err := c.claudeInfo(ctx, "--help")
	if err != nil {
		flags.Error = "claude --help: " + err.Error()
		subs.Error = flags.Error
		return flags, subs
	}
	f, s := parseHelp(string(root), "")
	flags.Items = append(flags.Items, f...)
	subs.Items = append(subs.Items, s...)

	for _, sc := range cli.Subcommands {
		if sc.NoHelpProbe {
			continue
		}
		out, err := c.claudeInfo(ctx, sc.Name, "--help")
		if err != nil {
			c.logf("claude %s --help: %v", sc.Name, err)
			continue
		}
		if !helpIsFor(string(out), sc.Name) {
			// claude printed its root help: the subcommand is gone.
			c.logf("claude %s --help printed the root help", sc.Name)
			continue
		}
		f, s := parseHelp(string(out), sc.Name)
		flags.Items = append(flags.Items, f...)
		subs.Items = append(subs.Items, s...)
		if sc.Hidden {
			subs.Items = append(subs.Items, Item{Name: sc.Name, Attrs: map[string]string{"hidden": "true"}})
		}
	}
	return flags, subs
}

var (
	// An array literal of at least five "family:action" strings.
	actionArrayRE = regexp.MustCompile(`\["[a-z][A-Za-z]*:[A-Za-z0-9]+"(?:,"[a-z][A-Za-z]*:[A-Za-z0-9]+"){4,}\]`)
	// An array literal of at least ten capitalized words.
	contextArrayRE = regexp.MustCompile(`\["[A-Z][A-Za-z]+"(?:,"[A-Z][A-Za-z]+"){9,}\]`)
	contextKeyRE   = regexp.MustCompile(`context:"([A-Z][A-Za-z]+)"`)
	commandRE      = regexp.MustCompile(`\.command\("([a-z][a-z0-9-]*)`)
	quotedRE       = regexp.MustCompile(`"([^"]*)"`)
)

// URL-like and module prefixes that look like "family:action" but aren't.
var notActionFamilies = []string{"node", "bun", "data", "http", "https", "file", "git", "npm", "mailto"}

// collectBinary scans claude's executable (a Bun bundle with readable JS) for keybinding
// action IDs, keybinding contexts and command names.
func (c *Collector) collectBinary(subs List) (actions, contexts, candidates List) {
	actions = List{Kind: KindAction, Source: "claude binary"}
	contexts = List{Kind: KindContext, Source: "claude binary"}
	candidates = List{Kind: KindSubcommandCandidate, Source: "claude binary"}
	path, err := c.binaryPath()
	if err == nil {
		var data []byte
		data, err = os.ReadFile(path)
		if err == nil {
			actions.Items, contexts.Items, candidates.Items = scanBinary(data, subs)
			return
		}
	}
	actions.Error, contexts.Error, candidates.Error = err.Error(), err.Error(), err.Error()
	return
}

func (c *Collector) binaryPath() (string, error) {
	if c.Binary != "" {
		return c.Binary, nil
	}
	if c.Claude == "" {
		return "", errors.New("claude not found")
	}
	return filepath.EvalSymlinks(c.Claude)
}

// scanBinary extracts the keybinding lists and the command names that no help output
// mentioned (subs holds every subcommand seen in help).
func scanBinary(data []byte, subs List) (actions, contexts, candidates []Item) {
	for _, arr := range actionArrayRE.FindAll(data, -1) {
		for _, m := range quotedRE.FindAllSubmatch(arr, -1) {
			id := string(m[1])
			family, _, _ := strings.Cut(id, ":")
			if slices.Contains(notActionFamilies, family) {
				continue
			}
			actions = append(actions, Item{Name: id})
		}
	}
	for _, arr := range contextArrayRE.FindAll(data, -1) {
		if !bytes.Contains(arr, []byte(`"Global"`)) || !bytes.Contains(arr, []byte(`"Chat"`)) {
			continue
		}
		for _, m := range quotedRE.FindAllSubmatch(arr, -1) {
			contexts = append(contexts, Item{Name: string(m[1])})
		}
	}
	for _, m := range contextKeyRE.FindAllSubmatch(data, -1) {
		contexts = append(contexts, Item{Name: string(m[1])})
	}

	seen := map[string]bool{}
	for _, it := range subs.Items {
		seen[it.Name] = true
		for _, a := range strings.Split(it.Attrs["aliases"], ",") {
			seen[a] = true
		}
	}
	for _, s := range cli.Subcommands {
		seen[s.Name] = true
	}
	for _, m := range commandRE.FindAllSubmatch(data, -1) {
		if name := string(m[1]); !seen[name] {
			candidates = append(candidates, Item{Name: name})
		}
	}
	return actions, contexts, candidates
}

// collectSettings fetches the settings schema (caching it) and lists its keys. Offline,
// or when the fetch fails, it uses the cached copy.
func (c *Collector) collectSettings(ctx context.Context) List {
	l := List{Kind: KindSetting, Source: SettingsSchemaURL}
	cached := filepath.Join(c.Cache, "claude-code-settings.json")
	var data []byte
	var fetchErr error
	if !c.Offline {
		data, fetchErr = c.fetch(ctx, SettingsSchemaURL)
		if fetchErr == nil && c.Cache != "" {
			if err := os.MkdirAll(c.Cache, 0o755); err == nil {
				_ = os.WriteFile(cached, data, 0o644)
			}
		}
	}
	if data == nil {
		var err error
		data, err = os.ReadFile(cached)
		if err != nil {
			l.Error = "no settings schema: " + errors.Join(fetchErr, err).Error()
			return l
		}
		l.Source += " (cached)"
		if fetchErr != nil {
			c.logf("settings schema: %v; using the cached copy", fetchErr)
		}
	}
	keys, err := schemaKeys(data)
	if err != nil {
		l.Error = err.Error()
		return l
	}
	for _, k := range keys {
		l.Items = append(l.Items, Item{Name: k})
	}
	return l
}

func (c *Collector) fetch(ctx context.Context, url string) ([]byte, error) {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// schemaKeys lists a JSON schema's top-level property names, plus "parent.child" for
// properties of object-valued properties. Local $refs are followed.
func schemaKeys(data []byte) ([]string, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("settings schema: %w", err)
	}
	props, _ := resolveRef(root, root)["properties"].(map[string]any)
	if props == nil {
		return nil, errors.New("settings schema has no properties")
	}
	var keys []string
	for k, v := range props {
		keys = append(keys, k)
		sub, _ := v.(map[string]any)
		if sub == nil {
			continue
		}
		if child, ok := resolveRef(root, sub)["properties"].(map[string]any); ok {
			for ck := range child {
				keys = append(keys, k+"."+ck)
			}
		}
	}
	slices.Sort(keys)
	return keys, nil
}

// resolveRef follows "#/a/b" references inside root, a few levels deep.
func resolveRef(root, node map[string]any) map[string]any {
	for range 8 {
		ref, ok := node["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, "#/") {
			return node
		}
		var cur any = root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, _ := cur.(map[string]any)
			cur = m[part]
		}
		next, ok := cur.(map[string]any)
		if !ok {
			return node
		}
		node = next
	}
	return node
}
