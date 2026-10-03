# Request 11 → 01: the command-line call site in cmd/mantle-ui

**Status:** resolved 2026-10-03: wired in `cmd/mantle-ui/main.go` as below, with plan 06's
sessions index as the resolver and the config store built from `st.FlagSettings()`.

Plan 11's B1 is ready on branch `worktree-mantle-11` (`internal/cli`, `features/cli`).
`cmd/mantle-ui/main.go` (plan 01, B9) needs this at its one call site, after matching
mantle-ui's own words (`story`, `catalog`, `selftest`), which would otherwise be a prompt:

```go
ctx := context.Background()
p, done, code := cli.Dispatch(ctx, os.Args[1:], os.Stdout, os.Stderr)
if done { // exec claude (-p, subcommands), --version, --help, usage errors (exit 64)
	os.Exit(code)
}
cwd, _ := os.Getwd()
st, err := p.Startup(cwd, resolver) // resolver: plan 06's index, see below; nil is allowed
if err != nil { // e.g. -c with no session, -r <id> that doesn't exist
	fmt.Fprintln(os.Stderr, "mantle:", err)
	os.Exit(1)
}
cli.SetCurrent(st) // read by features/cli, the gates (05), the picker (06), the editor (04)

opts.MainSpawn = st.MainSpawn() // nil when -r opens the picker first
opts.Session = st.Session
opts.A11y.ScreenReader = opts.A11y.ScreenReader || st.ScreenReader
if st.Safe { os.Setenv(ext.EnvSafe, "1") } // mantle --safe reached mantle-ui directly
```

Resolver for plan 06's `internal/sessions` (once merged):

```go
resolver := cli.ResolverFuncs{
	ContinueFunc: func(cwd string) (string, error) { m, err := ix.Continue(cwd); return m.ID, err },
	ResolveFunc: func(cwd, arg string) (string, string, error) {
		r, err := ix.Resolve(cwd, arg)
		if err != nil { return "", "", err }
		if r.Session != nil { return r.Session.ID, "", nil }
		return "", r.Query, nil
	},
}
```

mantle's own config store should take its flag scope from `st.FlagSettings()` instead of the
raw `--settings` value: it is the user's `--settings` plus `{"verbose": true}` for
`--verbose`, as claude applies it (plan 03 reads `Settings().Claude("verbose")`):

```go
flag, err := st.FlagSettings()
store := config.NewStore(paths, flag)
```

`st.Prompt` is submitted by `features/cli` when the main engine first attaches (after the
gates); nothing else is needed for it. `st.Verbose` and `st.PromptSuggestions` are there for
the verbose view and `initialize.promptSuggestions` (plan 02).
