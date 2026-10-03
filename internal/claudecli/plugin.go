package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// InstalledPlugin is one entry of "claude plugin list --json".
type InstalledPlugin struct {
	ID             string `json:"id"` // name@marketplace
	Version        string `json:"version"`
	Scope          string `json:"scope"`
	Enabled        bool   `json:"enabled"`
	InstallPath    string `json:"installPath"`
	InstalledAt    string `json:"installedAt"`
	LastUpdated    string `json:"lastUpdated"`
	ProjectEnabled bool   `json:"projectEnabled"`
}

// Name is the plugin name without "@marketplace".
func (p InstalledPlugin) Name() string { n, _ := splitPluginID(p.ID); return n }

// Marketplace is the marketplace part of the ID.
func (p InstalledPlugin) Marketplace() string { _, m := splitPluginID(p.ID); return m }

// AvailablePlugin is one entry of "claude plugin list --available --json".
type AvailablePlugin struct {
	PluginID        string       `json:"pluginId"`
	Name            string       `json:"name"`
	Description     string       `json:"description"`
	MarketplaceName string       `json:"marketplaceName"`
	Version         string       `json:"version"`
	InstallCount    int          `json:"installCount"`
	Source          PluginSource `json:"source"`
}

// PluginSource is where a marketplace fetches a plugin from. The CLI prints a
// relative path string or an object keyed by "source".
type PluginSource struct {
	Kind string // "path" for a plain string, otherwise the object's "source" (url, git-subdir, github, …)
	Path string
	URL  string
	Repo string
	Ref  string
	SHA  string
	Raw  json.RawMessage
}

func (s *PluginSource) UnmarshalJSON(b []byte) error {
	s.Raw = append(json.RawMessage(nil), b...)
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		s.Kind, s.Path = "path", str
		return nil
	}
	var obj struct {
		Source string `json:"source"`
		Path   string `json:"path"`
		URL    string `json:"url"`
		Repo   string `json:"repo"`
		Ref    string `json:"ref"`
		SHA    string `json:"sha"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil // unknown shape: keep Raw only
	}
	s.Kind, s.Path, s.URL, s.Repo, s.Ref, s.SHA = obj.Source, obj.Path, obj.URL, obj.Repo, obj.Ref, obj.SHA
	return nil
}

func (s PluginSource) MarshalJSON() ([]byte, error) {
	if len(s.Raw) > 0 {
		return s.Raw, nil
	}
	return []byte("null"), nil
}

// ParsePluginList parses "claude plugin list --json".
func ParsePluginList(b []byte) ([]InstalledPlugin, error) {
	var out []InstalledPlugin
	if err := json.Unmarshal(bytes.TrimSpace(b), &out); err != nil {
		return nil, fmt.Errorf("claude plugin list: %w", err)
	}
	return out, nil
}

// ParsePluginListAvailable parses "claude plugin list --available --json".
func ParsePluginListAvailable(b []byte) (installed []InstalledPlugin, available []AvailablePlugin, err error) {
	var out struct {
		Installed []InstalledPlugin `json:"installed"`
		Available []AvailablePlugin `json:"available"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(b), &out); err != nil {
		return nil, nil, fmt.Errorf("claude plugin list --available: %w", err)
	}
	return out.Installed, out.Available, nil
}

// Marketplace is one entry of "claude plugin marketplace list --json". The
// built-in directory and claude.ai-hosted marketplaces are not listed there.
type Marketplace struct {
	Name            string `json:"name"`
	Source          string `json:"source"` // github, git, url, directory, …
	Repo            string `json:"repo,omitempty"`
	URL             string `json:"url,omitempty"`
	Path            string `json:"path,omitempty"`
	InstallLocation string `json:"installLocation"`
}

// ParseMarketplaceList parses "claude plugin marketplace list --json".
func ParseMarketplaceList(b []byte) ([]Marketplace, error) {
	var out []Marketplace
	if err := json.Unmarshal(bytes.TrimSpace(b), &out); err != nil {
		return nil, fmt.Errorf("claude plugin marketplace list: %w", err)
	}
	return out, nil
}

// PluginResult is the one-line JSON result that plugin and marketplace mutations
// print with --json.
type PluginResult struct {
	Command      string        `json:"command"`
	Outcome      string        `json:"outcome"`
	Plugin       string        `json:"plugin"`
	Marketplace  string        `json:"marketplace"`
	Message      string        `json:"message"`
	FailureCode  string        `json:"failureCode"`
	ShownCommand *ShownCommand `json:"shownCommand"`
	Raw          json.RawMessage
}

// ShownCommand is a marketplace-declared command the CLI wants a person to
// confirm. Show it, then re-run with PluginInstallOptions.AcceptCommand = SHA256.
type ShownCommand struct {
	SHA256  string `json:"sha256"`
	Command string `json:"command"`
	Raw     json.RawMessage
}

func (s *ShownCommand) UnmarshalJSON(b []byte) error {
	type plain ShownCommand
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		var str string
		if json.Unmarshal(b, &str) == nil {
			s.Command, s.Raw = str, append(json.RawMessage(nil), b...)
			return nil
		}
		return err
	}
	*s = ShownCommand(p)
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// Failed reports whether the result describes a failure.
func (r PluginResult) Failed() bool {
	return r.FailureCode != "" || strings.EqualFold(r.Outcome, "failed") || strings.EqualFold(r.Outcome, "error")
}

// NeedsConfirmation reports whether the CLI is waiting for a person to accept
// a marketplace-declared command.
func (r PluginResult) NeedsConfirmation() bool {
	return r.ShownCommand != nil && r.ShownCommand.SHA256 != ""
}

// ParsePluginResult finds the last JSON object line in stdout.
func ParsePluginResult(stdout []byte) (PluginResult, bool) {
	lines := bytes.Split(stdout, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var r PluginResult
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		r.Raw = append(json.RawMessage(nil), line...)
		return r, true
	}
	return PluginResult{}, false
}

// PluginError is a failed plugin or marketplace mutation.
type PluginError struct {
	Result PluginResult
	Err    error // the underlying *ExitError, if the process exited non-zero
}

func (e *PluginError) Error() string {
	if e.Result.Message != "" {
		return fmt.Sprintf("claude plugin %s: %s", e.Result.Command, e.Result.Message)
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("claude plugin %s: %s", e.Result.Command, e.Result.Outcome)
}

func (e *PluginError) Unwrap() error { return e.Err }

// mutate runs a --json mutation and folds the exit status and the result line
// into one error.
func (r *Runner) mutate(ctx context.Context, sub Subcommand, args []string, stdin []byte) (PluginResult, error) {
	res, err := r.Do(ctx, Call{Sub: sub, Args: append([]string{"--json"}, args...), Stdin: stdin})
	result, ok := ParsePluginResult(res.Stdout)
	var argErr *ArgError
	switch {
	case errors.As(err, &argErr):
		return result, err
	case ok && (err != nil || result.Failed()):
		return result, &PluginError{Result: result, Err: err}
	case err != nil:
		return result, err
	}
	return result, nil
}

// PluginList runs "claude plugin list --json".
func (r *Runner) PluginList(ctx context.Context) ([]InstalledPlugin, error) {
	out, _, err := r.Run(ctx, PluginList, "--json")
	if err != nil {
		return nil, err
	}
	return ParsePluginList(out)
}

// PluginListAvailable runs "claude plugin list --available --json".
func (r *Runner) PluginListAvailable(ctx context.Context) ([]InstalledPlugin, []AvailablePlugin, error) {
	out, _, err := r.Run(ctx, PluginList, "--json", "--available")
	if err != nil {
		return nil, nil, err
	}
	return ParsePluginListAvailable(out)
}

// PluginDetails runs "claude plugin details <name>" and returns its text.
func (r *Runner) PluginDetails(ctx context.Context, plugin string) (string, error) {
	out, _, err := r.Run(ctx, PluginDetails, "--", plugin)
	return string(out), err
}

// PluginInstallOptions describes "claude plugin install".
type PluginInstallOptions struct {
	Plugin string // name or name@marketplace
	Scope  string // user (default), project or local
	// AcceptCommand accepts exactly the marketplace-declared command whose
	// sha256 an earlier result reported in ShownCommand. Mantle never passes a
	// blanket --yes for installs: the person sees the command first.
	AcceptCommand string
	Config        []string // key=value userConfig options
	Registry      string
}

// PluginInstall runs "claude plugin install --json". When the result
// NeedsConfirmation, show ShownCommand and call again with AcceptCommand.
func (r *Runner) PluginInstall(ctx context.Context, o PluginInstallOptions) (PluginResult, error) {
	if o.AcceptCommand != "" && !reSHA256.MatchString(o.AcceptCommand) {
		return PluginResult{}, &ArgError{Sub: PluginInstall, Msg: "accept-command must be a sha256"}
	}
	args := appendValue(nil, "--scope", o.Scope)
	args = appendValue(args, "--accept-command", o.AcceptCommand)
	for _, c := range o.Config {
		args = append(args, "--config="+c)
	}
	args = appendValue(args, "--registry", o.Registry)
	return r.mutate(ctx, PluginInstall, append(args, "--", o.Plugin), nil)
}

// PluginUninstall runs "claude plugin uninstall --json". Prune also removes
// auto-installed dependencies (that needs --yes when not on a TTY).
func (r *Runner) PluginUninstall(ctx context.Context, plugin, scope string, keepData, prune bool) (PluginResult, error) {
	args := appendValue(nil, "--scope", scope)
	if keepData {
		args = append(args, "--keep-data")
	}
	if prune {
		args = append(args, "--prune", "--yes")
	}
	return r.mutate(ctx, PluginUninstall, append(args, "--", plugin), nil)
}

// PluginEnable runs "claude plugin enable --json". An empty scope auto-detects.
func (r *Runner) PluginEnable(ctx context.Context, plugin, scope string) (PluginResult, error) {
	return r.mutate(ctx, PluginEnable, append(appendValue(nil, "--scope", scope), "--", plugin), nil)
}

// PluginDisable runs "claude plugin disable --json". An empty plugin with all
// set disables every plugin.
func (r *Runner) PluginDisable(ctx context.Context, plugin, scope string, all bool) (PluginResult, error) {
	args := appendValue(nil, "--scope", scope)
	if all {
		args = append(args, "--all")
	}
	if plugin != "" {
		args = append(args, "--", plugin)
	}
	return r.mutate(ctx, PluginDisable, args, nil)
}

// PluginUpdate runs "claude plugin update --json". A restart applies it.
func (r *Runner) PluginUpdate(ctx context.Context, plugin, scope, acceptCommand string) (PluginResult, error) {
	if acceptCommand != "" && !reSHA256.MatchString(acceptCommand) {
		return PluginResult{}, &ArgError{Sub: PluginUpdate, Msg: "accept-command must be a sha256"}
	}
	args := appendValue(nil, "--scope", scope)
	args = appendValue(args, "--accept-command", acceptCommand)
	return r.mutate(ctx, PluginUpdate, append(args, "--", plugin), nil)
}

// PluginConfig is "claude plugin configure --json <plugin>".
type PluginConfig struct {
	PluginID     string                     `json:"pluginId"`
	DisplayName  string                     `json:"displayName"`
	Schema       map[string]json.RawMessage `json:"schema"`
	Inputs       map[string]json.RawMessage `json:"inputs"`
	Choices      map[string]json.RawMessage `json:"choices"`
	Configured   []json.RawMessage          `json:"configured"`
	Unconfigured []json.RawMessage          `json:"unconfigured"`
}

// PluginOption is the usual shape of one schema entry.
type PluginOption struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Sensitive   bool            `json:"sensitive"`
	Required    bool            `json:"required"`
	Default     json.RawMessage `json:"default"`
}

// Option decodes one schema entry; ok is false when it has another shape.
func (c PluginConfig) Option(key string) (PluginOption, bool) {
	var o PluginOption
	raw, found := c.Schema[key]
	if !found || json.Unmarshal(raw, &o) != nil {
		return PluginOption{}, false
	}
	return o, true
}

// ParsePluginConfig parses "claude plugin configure --json".
func ParsePluginConfig(b []byte) (PluginConfig, error) {
	var c PluginConfig
	if err := json.Unmarshal(bytes.TrimSpace(b), &c); err != nil {
		return c, fmt.Errorf("claude plugin configure: %w", err)
	}
	return c, nil
}

// PluginConfigure runs "claude plugin configure --json <plugin>".
func (r *Runner) PluginConfigure(ctx context.Context, plugin string) (PluginConfig, error) {
	out, _, err := r.Run(ctx, PluginConfigure, "--json", "--", plugin)
	if err != nil {
		return PluginConfig{}, err
	}
	return ParsePluginConfig(out)
}

// PluginConfigureSet saves option values through --values-stdin. Values must be
// single-line strings; options left out keep their values.
func (r *Runner) PluginConfigureSet(ctx context.Context, plugin string, values map[string]string) (PluginConfig, error) {
	for k, v := range values {
		if strings.ContainsAny(v, "\n\r") {
			return PluginConfig{}, &ArgError{Sub: PluginConfigure, Msg: fmt.Sprintf("value for %q must be one line", k)}
		}
	}
	in, err := json.Marshal(values)
	if err != nil {
		return PluginConfig{}, err
	}
	res, err := r.Do(ctx, Call{Sub: PluginConfigure, Args: []string{"--json", "--values-stdin", "--", plugin}, Stdin: in})
	if err != nil {
		return PluginConfig{}, err
	}
	c, perr := ParsePluginConfig(res.Stdout)
	if perr != nil {
		return PluginConfig{}, nil // saved; the CLI printed something other than the config
	}
	return c, nil
}

// MarketplaceList runs "claude plugin marketplace list --json".
func (r *Runner) MarketplaceList(ctx context.Context) ([]Marketplace, error) {
	out, _, err := r.Run(ctx, MarketplaceList, "--json")
	if err != nil {
		return nil, err
	}
	return ParseMarketplaceList(out)
}

// MarketplaceAdd runs "claude plugin marketplace add --json <source>".
func (r *Runner) MarketplaceAdd(ctx context.Context, source, scope string, claudeAI bool, sparse []string) (PluginResult, error) {
	args := appendValue(nil, "--scope", scope)
	if claudeAI {
		args = append(args, "--claudeai")
	}
	for _, s := range sparse {
		args = append(args, "--sparse="+s)
	}
	return r.mutate(ctx, MarketplaceAdd, append(args, "--", source), nil)
}

// MarketplaceRemove runs "claude plugin marketplace remove --json <name>". An
// empty scope removes it from every scope.
func (r *Runner) MarketplaceRemove(ctx context.Context, name, scope string) (PluginResult, error) {
	return r.mutate(ctx, MarketplaceRemove, append(appendValue(nil, "--scope", scope), "--", name), nil)
}

// MarketplaceUpdate runs "claude plugin marketplace update [name]". With a name
// it asks for a --json result line; without one it updates all marketplaces.
func (r *Runner) MarketplaceUpdate(ctx context.Context, name string) (PluginResult, error) {
	if name == "" {
		_, _, err := r.Run(ctx, MarketplaceUpdate)
		return PluginResult{}, err
	}
	return r.mutate(ctx, MarketplaceUpdate, []string{"--", name}, nil)
}

func splitPluginID(id string) (name, marketplace string) {
	if i := strings.LastIndexByte(id, '@'); i > 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}
