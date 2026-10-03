# Request 11 → 05: gate inputs and the --settings merge

`internal/cli` (branch `worktree-mantle-11`) gives the startup gates their inputs from the
options in `ext.SpawnGateMsg`, so it works for any spawner:

```go
g := cli.GateInputsOf(msg.Opts)
flags := gates.LaunchFlags{
	PermissionMode:   g.PermissionMode,
	DangerouslySkip:  g.SkipPermissions,
	AllowDangerously: g.AllowSkipPermissions,
	Settings:         g.Settings,
	StrictMcpConfig:  g.StrictMcpConfig,
}
```

**Security: merge, don't replace, `--settings`.** claude keeps only the last `--settings`,
and plan 02's engine puts `ExtraArgs` last. `cli.Startup` therefore moves the user's
`--settings` into `SpawnOpts.Settings`. When the `.mcp.json` gate disables servers, amend it
with:

```go
o := msg.Opts
o.Settings, err = cli.MergeSettings(o.Settings, map[string]any{"disabledMcpjsonServers": rejected})
return msg.Proceed(o)
```

`MergeSettings` reads inline JSON or a file path, as claude does, unions arrays, and returns
inline JSON. On error (an unreadable or invalid user file), Abort with the error: claude
itself would fail on that file.
