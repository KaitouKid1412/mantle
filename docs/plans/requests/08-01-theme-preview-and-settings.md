# Request 08 → 01: theme preview, per-scope settings, config writer

From plan 08 (settings panels), for plan 01 (API steward). All additive.

## 1. Theme preview message (needed by /theme, B6)

The /theme picker previews the highlighted theme live and restores the original on
Esc. The host owns the active theme, so the picker needs a message the host applies
without persisting anything:

```go
// ThemePreviewMsg temporarily shows a theme (and optionally the syntax-highlighting
// toggle) without changing settings. Name "" ends the preview and restores the theme
// from settings.
type ThemePreviewMsg struct {
	Name            string
	SyntaxHighlight *bool // nil = unchanged
}
```

On confirm, plan 08 writes `theme` (and `syntaxHighlightingDisabled`) to user settings;
the normal settings reload then makes it permanent, after which plan 08 sends
`ThemePreviewMsg{Name: ""}`.

## 2. Per-scope settings (needed by /permissions B8 and /status B5)

`ext.Settings.Claude` returns merged values only. /permissions shows each rule with
its source and edits one scope; /status lists the settings files in effect and flags
invalid ones. Proposed additive methods on `ext.Settings` (mantle implements it):

```go
// ClaudeScope returns one scope's document as read from disk ("userSettings",
// "projectSettings", "localSettings", "flagSettings", "policySettings"); nil if absent.
ClaudeScope(scope string) map[string]any
// ClaudeSources describes every settings source the reader looked at.
ClaudeSources() []SettingsSource // {Scope, Path string; Exists bool; Err string}
```

Until then plan 08 reads the scope files itself (read-only), which duplicates the
reader's path logic (managed paths, `CLAUDE_CONFIG_DIR`).

## 3. Config writer properties

Plan 08 needed to write settings before B6 landed, so it has a stand-in at
`features/settings/settingsfile` (on branch `worktree-plan-08-settings`). Whatever the
final writer looks like, the panels rely on these properties; feel free to lift the
package into `internal/config` wholesale:

- exclusive flock (lock files under `~/.mantle/locks`, not next to the user's file);
- re-read and apply the change to the freshest contents just before writing;
- atomic temp file + rename, keeping the file's mode;
- **write through symlinks** (dotfile repos symlink `~/.claude/settings.json`);
- **keep key order and two-space indentation**, append new keys, keep numbers verbatim
  (`json.Number`), no HTML escaping — otherwise every write reshuffles the user's file;
- refuse to overwrite a file that is not a JSON object (the engine ignores invalid
  files in `-p`, so clobbering one loses the user's settings);
- refuse `~/.claude.json` and `$CLAUDE_CONFIG_DIR/.claude.json`, including via symlink.

The API plan 08 calls is `Update(path, func(map[string]any) (map[string]any, error))`
plus `Apply(patch.Patch)`, where a patch is a scope plus set/delete/add/remove ops.
