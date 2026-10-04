// Command sdk-diff compares the protocol unions of the Agent SDK built for the
// installed claude with the tables in pkg/proto, and reports the differences:
// message types, system and result subtypes, and control request subtypes that the
// SDK declares and pkg/proto doesn't ("sdk-only"), or the reverse ("proto-only").
//
//	go run ./scripts/sdk-diff                 # human report
//	go run ./scripts/sdk-diff -json           # [{"name":"system:foo","kind":"sdk-only"}, ...]
//	go run ./scripts/sdk-diff -sdk 0.3.290    # a specific SDK version
//	go run ./scripts/sdk-diff -file sdk.d.ts  # a local typings file (offline)
//
// The SDK for CLI 2.1.<patch> is @anthropic-ai/claude-agent-sdk@0.3.<patch>. The tarball
// is downloaded from the npm registry into the user cache directory, never into the
// repository (license: the typings are not ours to commit).
//
// Primary owner: plan 02.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sdk-diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print a JSON array of {name, kind}")
	sdkVer := fs.String("sdk", "", "SDK version (default: 0.3.<patch> of `claude --version`)")
	file := fs.String("file", "", "read this sdk.d.ts instead of downloading")
	cache := fs.String("cache", "", "download cache (default: <user cache>/mantle/sdk-diff)")
	registry := fs.String("registry", "https://registry.npmjs.org", "npm registry")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	src, ver, err := loadTypings(*file, *sdkVer, *cache, *registry)
	if err != nil {
		fmt.Fprintln(stderr, "sdk-diff:", err)
		return 1
	}
	sdk := fromSDK(src)
	if len(sdk.Types) == 0 || len(sdk.ControlSubs) == 0 {
		fmt.Fprintln(stderr, "sdk-diff: found no protocol unions in the typings (format changed?)")
		return 1
	}
	items := diff(sdk, fromProto())
	if *asJSON {
		if items == nil {
			items = []Item{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "SDK %s vs pkg/proto: %d types, %d system subtypes, %d result subtypes, %d control subtypes in the SDK\n",
		ver, len(sdk.Types), len(sdk.SystemSubtypes), len(sdk.ResultSubtypes), len(sdk.ControlSubs))
	if len(items) == 0 {
		fmt.Fprintln(stdout, "no differences")
	}
	for _, it := range items {
		fmt.Fprintf(stdout, "  %-10s %s\n", it.Kind, it.Name)
	}
	if len(sdk.Unresolved) > 0 {
		fmt.Fprintf(stdout, "unresolved union members (check by hand): %s\n", strings.Join(sdk.Unresolved, ", "))
	}
	return 0
}

// fromProto lists pkg/proto's tables in the same shape.
func fromProto() Protocol {
	p := newProtocol()
	for _, t := range proto.KnownTypes() {
		p.Types[t] = true
	}
	for _, s := range proto.KnownSystemSubtypes() {
		p.SystemSubtypes[s] = true
	}
	for _, s := range proto.KnownResultSubtypes() {
		p.ResultSubtypes[s] = true
	}
	for _, s := range proto.KnownRequestSubtypes() {
		p.ControlSubs[s] = true
	}
	for _, s := range proto.KnownCLIRequestSubtypes() {
		p.ControlSubs[s] = true
	}
	return p
}

// loadTypings returns sdk.d.ts and the SDK version it came from.
func loadTypings(file, ver, cache, registry string) (string, string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		return string(b), file, err
	}
	if ver == "" {
		v, err := sdkForInstalledCLI()
		if err != nil {
			return "", "", err
		}
		ver = v
	}
	if cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return "", "", err
		}
		cache = filepath.Join(dir, "mantle", "sdk-diff")
	}
	path := filepath.Join(cache, "claude-agent-sdk-"+ver+".sdk.d.ts")
	if b, err := os.ReadFile(path); err == nil {
		return string(b), ver, nil
	}
	url := fmt.Sprintf("%s/@anthropic-ai/claude-agent-sdk/-/claude-agent-sdk-%s.tgz", strings.TrimRight(registry, "/"), ver)
	src, err := fetchTypings(url)
	if err != nil {
		return "", "", fmt.Errorf("SDK %s: %w", ver, err)
	}
	if err := os.MkdirAll(cache, 0o755); err == nil {
		_ = os.WriteFile(path, []byte(src), 0o644)
	}
	return src, ver, nil
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// sdkForInstalledCLI maps claude 2.1.<patch> to SDK 0.3.<patch>.
func sdkForInstalledCLI() (string, error) {
	bin := os.Getenv("MANTLE_CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = append(os.Environ(), "CLAUDECODE=")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude --version: %w (use -sdk or -file)", err)
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return "", fmt.Errorf("claude --version: no version in %q", out)
	}
	if m[1] != "2" || m[2] != "1" {
		return "", fmt.Errorf("claude %s.%s.%s: no known SDK mapping (use -sdk)", m[1], m[2], m[3])
	}
	return "0.3." + m[3], nil
}

// fetchTypings downloads the npm tarball and returns package/sdk.d.ts.
func fetchTypings(url string) (string, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return extractTypings(resp.Body)
}

func extractTypings(r io.Reader) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("package/sdk.d.ts not in the tarball")
		}
		if err != nil {
			return "", err
		}
		if h.Name == "package/sdk.d.ts" {
			b, err := io.ReadAll(io.LimitReader(tr, 64<<20))
			return string(b), err
		}
	}
}
