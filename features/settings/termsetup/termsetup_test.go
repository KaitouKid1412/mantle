package termsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "../../../testdata/fixtures/08/termsetup"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// replan runs the planner again on an edit's output; installers must be idempotent.
func replan(t *testing.T, in Input, p Proposal) Proposal {
	t.Helper()
	if p.Status != Edit {
		t.Fatalf("expected an edit, got %s: %s %v", p.Status, p.Summary, p.Notes)
	}
	in.Exists, in.Content = true, p.New
	return Plan(in)
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		goos string
		want Detected
	}{
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, "darwin", Detected{Terminal: Ghostty}},
		{"ghostty term", map[string]string{"TERM": "xterm-ghostty"}, "linux", Detected{Terminal: Ghostty}},
		{"iterm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "darwin", Detected{Terminal: ITerm2}},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "darwin", Detected{Terminal: AppleTerminal}},
		{"apple terminal off darwin", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "linux", Detected{}},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, "darwin", Detected{Terminal: WezTerm}},
		{"kitty", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, "linux", Detected{Terminal: Kitty}},
		{"alacritty", map[string]string{"TERM": "alacritty"}, "linux", Detected{Terminal: Alacritty}},
		{"alacritty socket", map[string]string{"TERM": "xterm-256color", "ALACRITTY_SOCKET": "/tmp/s"}, "darwin", Detected{Terminal: Alacritty}},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode", "__CFBundleIdentifier": "com.microsoft.VSCode"}, "darwin", Detected{Terminal: VSCode}},
		{"vscode default", map[string]string{"TERM_PROGRAM": "vscode"}, "linux", Detected{Terminal: VSCode}},
		{"insiders", map[string]string{"TERM_PROGRAM": "vscode", "__CFBundleIdentifier": "com.microsoft.VSCodeInsiders"}, "darwin", Detected{Terminal: VSCodeInsiders}},
		{"cursor bundle", map[string]string{"TERM_PROGRAM": "vscode", "__CFBundleIdentifier": "com.todesktop.230313mzl4w4u92"}, "darwin", Detected{Terminal: Cursor}},
		{"cursor askpass", map[string]string{"TERM_PROGRAM": "vscode", "VSCODE_GIT_ASKPASS_MAIN": "/Applications/Cursor.app/Contents/Resources/app/extensions/git/dist/askpass-main.js"}, "darwin", Detected{Terminal: Cursor}},
		{"windsurf", map[string]string{"TERM_PROGRAM": "vscode", "VSCODE_GIT_ASKPASS_MAIN": "/opt/Windsurf/resources/app/askpass-main.js"}, "linux", Detected{Terminal: Windsurf}},
		{"codium", map[string]string{"TERM_PROGRAM": "vscode", "VSCODE_GIT_ASKPASS_MAIN": "/usr/share/codium/resources/askpass-main.js"}, "linux", Detected{Terminal: VSCodium}},
		{"tmux in ghostty", map[string]string{"TERM_PROGRAM": "tmux", "TMUX": "/tmp/tmux-1/default,1,0", "GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app/Contents/Resources/ghostty"}, "darwin", Detected{Terminal: Ghostty, InTmux: true}},
		{"tmux in iterm", map[string]string{"TERM_PROGRAM": "tmux", "TMUX": "x", "LC_TERMINAL": "iTerm2"}, "darwin", Detected{Terminal: ITerm2, InTmux: true}},
		{"unknown", map[string]string{"TERM": "xterm-256color"}, "linux", Detected{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.env, tt.goos); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNativeAndManualTerminals(t *testing.T) {
	for _, term := range []Terminal{Ghostty, Kitty} {
		if p := Plan(Input{Terminal: term}); p.Status != Native || p.Kind != TargetNone {
			t.Errorf("%s: %s", term, p.Status)
		}
	}
	if p := Plan(Input{Terminal: WezTerm}); p.Status != Manual || !strings.Contains(strings.Join(p.Notes, " "), "enable_kitty_keyboard") {
		t.Errorf("wezterm: %+v", p)
	}
	for _, term := range []Terminal{WezTerm, ITerm2, AppleTerminal, VSCode, Cursor, Alacritty} {
		if p := Plan(Input{Terminal: term, KittyKeyboard: true}); p.Status != Native {
			t.Errorf("%s with kitty protocol: %s", term, p.Status)
		}
	}
	p := Plan(Input{Terminal: TerminalUnknown})
	if p.Status != Unsupported || len(p.Notes) == 0 {
		t.Errorf("unknown: %+v", p)
	}
}

func TestTmuxNote(t *testing.T) {
	p := Plan(Input{Terminal: Ghostty, InTmux: true})
	if !strings.Contains(strings.Join(p.Notes, "\n"), "extended-keys") {
		t.Errorf("no tmux note: %v", p.Notes)
	}
	if p := Plan(Input{Terminal: Ghostty}); len(p.Notes) != 0 {
		t.Errorf("unexpected notes: %v", p.Notes)
	}
}

func TestVSCodeComments(t *testing.T) {
	old := fixture(t, "vscode_comments.json")
	in := Input{Terminal: VSCode, Path: "keybindings.json", Exists: true, Content: old}
	p := Plan(in)
	if p.Status != Edit || p.Kind != TargetFile || p.Path != "keybindings.json" {
		t.Fatalf("got %s: %s %v", p.Status, p.Summary, p.Notes)
	}
	want := strings.Replace(string(old), "    },\n]\n", `    },
    {
        "key": "shift+enter",
        "command": "workbench.action.terminal.sendSequence",
        "args": {
            "text": "\u001b\r"
        },
        "when": "terminalFocus"
    }
]
`, 1)
	if string(p.New) != want {
		t.Errorf("new content:\n%s\nwant:\n%s", p.New, want)
	}
	if !strings.Contains(p.Diff, "+        \"key\": \"shift+enter\",") {
		t.Errorf("diff:\n%s", p.Diff)
	}
	var entries []vscodeEntry
	if err := parseJSONC(p.New, &entries); err != nil || len(entries) != 3 {
		t.Fatalf("result doesn't parse: %v (%d entries)", err, len(entries))
	}
	if text, _ := sendSequenceText(entries[2].Args); text != NewlineSeq {
		t.Errorf("sequence %q", text)
	}
	if q := replan(t, in, p); q.Status != AlreadyInstalled {
		t.Errorf("replan: %s", q.Status)
	}
}

func TestVSCodeShapes(t *testing.T) {
	tests := []struct {
		name    string
		exists  bool
		content string
		status  Status
		want    string // expected New for edits
	}{
		{"missing file", false, "", Edit, "[\n    {\n        \"key\": \"shift+enter\",\n        \"command\": \"workbench.action.terminal.sendSequence\",\n        \"args\": {\n            \"text\": \"\\u001b\\r\"\n        },\n        \"when\": \"terminalFocus\"\n    }\n]\n"},
		{"default", true, string(fixture(t, "vscode_default.json")), Edit, "[\n    {\n        \"key\": \"shift+enter\",\n        \"command\": \"workbench.action.terminal.sendSequence\",\n        \"args\": {\n            \"text\": \"\\u001b\\r\"\n        },\n        \"when\": \"terminalFocus\"\n    }\n]\n"},
		{"only a comment", true, "// nothing yet", Edit, "// nothing yet\n[\n    {\n        \"key\": \"shift+enter\",\n        \"command\": \"workbench.action.terminal.sendSequence\",\n        \"args\": {\n            \"text\": \"\\u001b\\r\"\n        },\n        \"when\": \"terminalFocus\"\n    }\n]\n"},
		{"one line", true, `[{"key":"a","command":"b"}]`, Edit, "[{\"key\":\"a\",\"command\":\"b\"},\n    {\n        \"key\": \"shift+enter\",\n        \"command\": \"workbench.action.terminal.sendSequence\",\n        \"args\": {\n            \"text\": \"\\u001b\\r\"\n        },\n        \"when\": \"terminalFocus\"\n    }\n]"},
		{"tab indented", true, "[\n\t{\"key\": \"a\", \"command\": \"b\"}\n]\n", Edit, "[\n\t{\"key\": \"a\", \"command\": \"b\"},\n\t{\n\t\t\"key\": \"shift+enter\",\n\t\t\"command\": \"workbench.action.terminal.sendSequence\",\n\t\t\"args\": {\n\t\t\t\"text\": \"\\u001b\\r\"\n\t\t},\n\t\t\"when\": \"terminalFocus\"\n\t}\n]\n"},
		{"installed", true, string(fixture(t, "vscode_installed.json")), AlreadyInstalled, ""},
		{"line feed counts", true, `[{"key":"shift+enter","command":"workbench.action.terminal.sendSequence","args":{"text":"\n"},"when":"terminalFocus"}]`, AlreadyInstalled, ""},
		{"conflict", true, string(fixture(t, "vscode_conflict.json")), Manual, ""},
		{"other sequence", true, `[{"key":"shift+enter","command":"workbench.action.terminal.sendSequence","args":{"text":"\\\r\n"},"when":"terminalFocus"}]`, Manual, ""},
		{"removal entry ignored", true, `[{"key":"shift+enter","command":"-workbench.action.terminal.foo","when":"terminalFocus"}]`, Edit, ""},
		{"not an array", true, `{"key":"x"}`, Manual, ""},
		{"broken", true, `[{"key": }]`, Manual, ""},
		{"comment with bracket", true, "[\n  // ] not the end /* nor this\n  {\"key\":\"a\",\"command\":\"b\"} /* ] */\n]\n", Edit, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Terminal: Cursor, Path: "kb.json", Exists: tt.exists, Content: []byte(tt.content)}
			p := Plan(in)
			if p.Status != tt.status {
				t.Fatalf("status %s, want %s (%s %v)", p.Status, tt.status, p.Summary, p.Notes)
			}
			if p.Status != Edit {
				return
			}
			if tt.want != "" && string(p.New) != tt.want {
				t.Errorf("new:\n%q\nwant:\n%q", p.New, tt.want)
			}
			var entries []vscodeEntry
			if err := parseJSONC(p.New, &entries); err != nil {
				t.Fatalf("result doesn't parse: %v\n%s", err, p.New)
			}
			if q := replan(t, in, p); q.Status != AlreadyInstalled {
				t.Errorf("replan: %s %s", q.Status, q.Summary)
			}
		})
	}
}

func TestITerm2AddsKeyMap(t *testing.T) {
	old := fixture(t, "iterm2_nokeymap.plist")
	in := Input{Terminal: ITerm2, Path: "iterm2.plist", Exists: true, Content: old}
	p := Plan(in)
	if p.Status != Edit || p.Kind != TargetDefaults || p.Domain != "com.googlecode.iterm2" {
		t.Fatalf("got %+v", p)
	}
	want := strings.Replace(string(old), "\t<string>~/iterm-prefs</string>\n", `	<string>~/iterm-prefs</string>
	<key>GlobalKeyMap</key>
	<dict>
		<key>0xd-0x20000-0x24</key>
		<dict>
			<key>Action</key>
			<integer>11</integer>
			<key>Text</key>
			<string>0x1b 0x0d</string>
		</dict>
	</dict>
`, 1)
	if string(p.New) != want {
		t.Errorf("new:\n%s\nwant:\n%s", p.New, want)
	}
	if q := replan(t, in, p); q.Status != AlreadyInstalled {
		t.Errorf("replan: %s", q.Status)
	}
}

func TestITerm2ExistingKeyMap(t *testing.T) {
	old := fixture(t, "iterm2_keymap.plist")
	in := Input{Terminal: ITerm2, Exists: true, Content: old}
	p := Plan(in)
	want := strings.Replace(string(old), "\t\t\t<string>ctrl-return</string>\n\t\t</dict>\n", `			<string>ctrl-return</string>
		</dict>
		<key>0xd-0x20000-0x24</key>
		<dict>
			<key>Action</key>
			<integer>11</integer>
			<key>Text</key>
			<string>0x1b 0x0d</string>
		</dict>
`, 1)
	if string(p.New) != want {
		t.Errorf("new:\n%s\nwant:\n%s", p.New, want)
	}
	root, err := parsePlist(p.New)
	if err != nil {
		t.Fatal(err)
	}
	if km := root.get("GlobalKeyMap"); km == nil || len(km.keys) != 3 {
		t.Errorf("key map: %+v", km)
	}
	if q := replan(t, in, p); q.Status != AlreadyInstalled {
		t.Errorf("replan: %s", q.Status)
	}
}

func TestITerm2Existing(t *testing.T) {
	if p := Plan(Input{Terminal: ITerm2, Exists: true, Content: fixture(t, "iterm2_installed.plist")}); p.Status != AlreadyInstalled {
		t.Errorf("installed: %s", p.Status)
	}
	p := Plan(Input{Terminal: ITerm2, Exists: true, Content: fixture(t, "iterm2_conflict.plist")})
	if p.Status != Manual || !strings.Contains(p.Summary, `"hello"`) {
		t.Errorf("conflict: %s %s", p.Status, p.Summary)
	}
	if p := Plan(Input{Terminal: ITerm2}); p.Status != Manual {
		t.Errorf("missing: %s", p.Status)
	}
	if p := Plan(Input{Terminal: ITerm2, Exists: true, Content: []byte("not xml")}); p.Status != Manual {
		t.Errorf("garbage: %s", p.Status)
	}
}

func TestShiftReturnKeys(t *testing.T) {
	for key, want := range map[string]bool{
		"0xd-0x20000-0x24":  true,
		"0xd-0x20000":       true,
		"0xd-0x20100-0x24":  true, // device-dependent bits are ignored
		"0xd-0x60000-0x24":  false,
		"0xd-0xa0000-0x24":  false,
		"0xd-0x0-0x24":      false,
		"0xa-0x20000":       false,
		"0xf700-0x260000":   false,
		"garbage":           false,
		"0xzz-0x20000-0x24": false,
	} {
		if got := isShiftReturn(key); got != want {
			t.Errorf("%s: got %v", key, got)
		}
	}
}

func TestAppleTerminal(t *testing.T) {
	old := fixture(t, "terminal_profiles.plist")
	in := Input{Terminal: AppleTerminal, Path: "Terminal.plist", Exists: true, Content: old}
	p := Plan(in)
	if p.Status != Edit || p.Domain != "com.apple.Terminal" {
		t.Fatalf("got %+v", p)
	}
	want := strings.Replace(string(old), "\t\t\t<string>Window Settings</string>\n", "\t\t\t<string>Window Settings</string>\n\t\t\t<key>useOptionAsMetaKey</key>\n\t\t\t<true/>\n", 1)
	want = strings.Replace(want, "\t\t\t<key>useOptionAsMetaKey</key>\n\t\t\t<false/>", "\t\t\t<key>useOptionAsMetaKey</key>\n\t\t\t<true/>", 1)
	if string(p.New) != want {
		t.Errorf("new:\n%s\nwant:\n%s", p.New, want)
	}
	if !strings.Contains(p.Summary, `"Basic" and "Pro"`) {
		t.Errorf("summary: %s", p.Summary)
	}
	if q := replan(t, in, p); q.Status != AlreadyInstalled {
		t.Errorf("replan: %s", q.Status)
	}
	u := Plan(Input{Terminal: AppleTerminal, Exists: true, Content: fixture(t, "terminal_unsaved.plist")})
	if u.Status != Manual || !strings.Contains(strings.Join(u.Notes, " "), `"Ocean"`) {
		t.Errorf("unsaved: %+v", u)
	}
}

func TestAlacritty(t *testing.T) {
	block := "[[keyboard.bindings]]\nkey = \"Return\"\nmods = \"Shift\"\nchars = \"\\u001b\\r\"\n"
	tests := []struct {
		name    string
		file    string
		exists  bool
		legacy  string
		status  Status
		wantNew string
	}{
		{"plain", "alacritty_plain.toml", true, "", Edit, string(fixture(t, "alacritty_plain.toml")) + "\n" + block},
		{"other bindings", "alacritty_bindings.toml", true, "", Edit, string(fixture(t, "alacritty_bindings.toml")) + "\n" + block},
		{"installed", "alacritty_installed.toml", true, "", AlreadyInstalled, ""},
		{"inline", "alacritty_inline.toml", true, "", Manual, ""},
		{"conflict", "alacritty_conflict.toml", true, "", Manual, ""},
		{"missing", "", false, "", Edit, block},
		{"legacy yaml", "", false, "/cfg/alacritty.yml", Manual, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Terminal: Alacritty, Path: "alacritty.toml", Exists: tt.exists, LegacyPath: tt.legacy}
			if tt.file != "" {
				in.Content = fixture(t, tt.file)
			}
			p := Plan(in)
			if p.Status != tt.status {
				t.Fatalf("status %s, want %s (%s)", p.Status, tt.status, p.Summary)
			}
			if p.Status == Edit {
				if string(p.New) != tt.wantNew {
					t.Errorf("new:\n%q\nwant:\n%q", p.New, tt.wantNew)
				}
				if q := replan(t, in, p); q.Status != AlreadyInstalled {
					t.Errorf("replan: %s", q.Status)
				}
			}
		})
	}
}

func TestTOMLHelpers(t *testing.T) {
	for in, want := range map[string]string{
		`"\u001b\r"`: NewlineSeq,
		`"\e\r"`:     NewlineSeq,
		`'raw\n'`:    `raw\n`,
		`"a\"b"`:     `a"b`,
	} {
		if got, ok := tomlString(in); !ok || got != want {
			t.Errorf("%s: got %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{`"\u00"`, `x`, `"\q"`} {
		if _, ok := tomlString(bad); ok {
			t.Errorf("%s accepted", bad)
		}
	}
	if got := stripTOMLComment(`chars = "#not" # yes`); got != `chars = "#not" ` {
		t.Errorf("comment: %q", got)
	}
}

func TestLocate(t *testing.T) {
	mac := Env{Home: "/h", GOOS: "darwin"}
	linux := Env{Home: "/h", GOOS: "linux", XDGConfigHome: "/x"}
	cases := []struct {
		t    Terminal
		e    Env
		want Location
	}{
		{VSCode, mac, Location{Kind: TargetFile, Path: "/h/Library/Application Support/Code/User/keybindings.json"}},
		{Cursor, linux, Location{Kind: TargetFile, Path: "/x/Cursor/User/keybindings.json"}},
		{VSCodeInsiders, Env{Home: "/h", GOOS: "linux"}, Location{Kind: TargetFile, Path: "/h/.config/Code - Insiders/User/keybindings.json"}},
		{ITerm2, mac, Location{Kind: TargetDefaults, Domain: "com.googlecode.iterm2", Path: "/h/Library/Preferences/com.googlecode.iterm2.plist"}},
		{AppleTerminal, mac, Location{Kind: TargetDefaults, Domain: "com.apple.Terminal", Path: "/h/Library/Preferences/com.apple.Terminal.plist"}},
		{Ghostty, mac, Location{Kind: TargetNone}},
		{Alacritty, linux, Location{Kind: TargetFile, Path: "/x/alacritty/alacritty.toml"}},
	}
	for _, c := range cases {
		if got := Locate(c.t, c.e, nil); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.t, got, c.want)
		}
	}
	exists := func(p string) bool { return p == "/h/.alacritty.toml" }
	if got := Locate(Alacritty, linux, exists); got.Path != "/h/.alacritty.toml" {
		t.Errorf("alacritty home toml: %+v", got)
	}
	yml := func(p string) bool { return p == "/h/.config/alacritty/alacritty.yml" }
	if got := Locate(Alacritty, linux, yml); got.Path != "/x/alacritty/alacritty.toml" || got.LegacyPath != "/h/.config/alacritty/alacritty.yml" {
		t.Errorf("alacritty legacy: %+v", got)
	}
}
