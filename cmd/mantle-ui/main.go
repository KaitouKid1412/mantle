// Command mantle-ui is the mantle terminal UI. The mantle launcher (cmd/mantle)
// supervises it; it can also run directly.
//
//	mantle-ui [claude flags] [prompt]      the UI
//	mantle-ui story <id> [--width N] [--ansi]
//	mantle-ui catalog [--json]
//	mantle-ui selftest
//
// Exit codes: 0 normal, 64 usage error (ext.ExitUsage), 75 restart requested
// (ext.ExitRestart), anything else a crash.
//
// Primary owner: plan 01.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	_ "github.com/KaitouKid1412/mantle/features/all"
	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	// mantle-ui's own words come first; otherwise `mantle-ui story` would be a prompt.
	if len(args) > 0 {
		switch args[0] {
		case "story":
			return cmdStory(args[1:], stdout, stderr)
		case "catalog":
			return cmdCatalog(args[1:], stdout, stderr)
		case "selftest":
			return cmdSelftest(args[1:], stdout, stderr)
		case "keyprobe":
			return cmdKeyprobe(args[1:], stdout, stderr)
		}
	}

	ctx := context.Background()
	p, done, code := cli.Dispatch(ctx, args, stdout, stderr) // the one cli call site
	if done {
		return code
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "mantle:", err)
		return 1
	}
	resolver := cli.ResolverFuncs{
		ContinueFunc: func(cwd string) (string, error) {
			m, err := ix.Continue(cwd)
			return m.ID, err
		},
		ResolveFunc: func(cwd, arg string) (string, string, error) {
			r, err := ix.Resolve(cwd, arg)
			if err != nil {
				return "", "", err
			}
			if r.Session != nil {
				return r.Session.ID, "", nil
			}
			return "", r.Query, nil
		},
	}
	st, err := p.Startup(cwd, resolver)
	if err != nil {
		fmt.Fprintln(stderr, "mantle:", err)
		return 1
	}
	cli.SetCurrent(st)
	if st.Safe {
		os.Setenv(ext.EnvSafe, "1")
	}
	return runUI(ctx, cwd, st, stdout, stderr)
}

func runUI(ctx context.Context, cwd string, st cli.Startup, stdout, stderr io.Writer) int {
	paths, err := config.DefaultPaths(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "mantle:", err)
		return 1
	}
	logger, closeLog := openLog(paths)
	defer closeLog()

	flag, err := st.FlagSettings()
	if err != nil {
		fmt.Fprintln(stderr, "mantle: --settings:", err)
		return ext.ExitUsage
	}
	store := config.NewStore(paths, flag)
	host := app.NewHost(ext.Pending(), app.HostOptions{
		Safe:     os.Getenv(ext.EnvSafe) == "1",
		Disabled: config.DisabledIDs(config.ReadDisabled(paths)),
		Core:     app.CoreFeatures(),
	})
	for _, r := range host.Reports() {
		logger.Warn("setup", "report", r.String())
	}
	store.SetSpecs(host.Settings())
	themes, themeErrs := config.LoadCustomThemes(paths.ThemesDir())
	for _, e := range themeErrs {
		logger.Warn("custom theme", "err", e)
	}
	ui := store.UI()

	var prog *tea.Program
	mgr := engine.NewManager(func(m tea.Msg) { prog.Send(m) })
	mgr.Logf = func(format string, args ...any) { logger.Debug(fmt.Sprintf(format, args...)) }
	mgr.RunDir = filepath.Join(paths.MantleDir, "run") // honours MANTLE_HOME; the launcher reads it

	root := app.New(app.Options{
		Host:          host,
		Settings:      store,
		Logger:        logger,
		A11y:          ext.Accessibility{ScreenReader: st.ScreenReader || ui.AxScreenReader || truthy(os.Getenv("CLAUDE_AX_SCREEN_READER")), ReducedMotion: ui.PrefersReducedMotion},
		KeymapSources: config.LoadKeymapSources(paths),
		StateDir:      paths.StateDir(),
		CustomThemes:  config.ThemeMap(themes),
		Session:       st.Session,
		MainSpawn:     st.MainSpawn(),
		Spawn: func(id string, o ext.SpawnOpts) error {
			_, err := mgr.Start(id, o)
			return err
		},
		Stop: func(id string) error {
			mgr.Remove(context.Background(), id)
			return nil
		},
		OnDisable: func(feature, reason string) {
			logger.Error("feature disabled", "feature", feature, "reason", reason)
			if err := config.RecordDisabled(paths, feature, reason, time.Now()); err != nil {
				logger.Error("record disabled", "err", err)
			}
		},
	})
	prog = tea.NewProgram(root, tea.WithContext(ctx))
	watcher, err := config.Watch(paths, flag, prog.Send)
	if err != nil {
		logger.Warn("settings watcher", "err", err)
	} else {
		defer watcher.Close()
	}

	_, runErr := prog.Run()

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	mgr.Close(stopCtx)
	cancel()
	if err := ix.Save(); err != nil {
		logger.Debug("session index", "err", err)
	}
	if runErr != nil {
		logger.Error("program", "err", runErr)
		fmt.Fprintln(stderr, "mantle:", runErr)
		return 1
	}
	if reason := root.ExitReason(); reason != "" && root.ExitCode() == 0 {
		fmt.Fprintln(stdout, reason)
	}
	return root.ExitCode()
}

// ix is shared so the index cache is saved on exit.
var ix *sessions.Index

func init() {
	ix = sessions.NewIndex(sessions.DefaultLayout(), sessions.DefaultCachePath())
}

// openLog opens ~/.mantle/logs/<pid>.log. mantle-ui never writes diagnostics to the
// terminal while the UI runs.
func openLog(p config.Paths) (*slog.Logger, func()) {
	level := slog.LevelInfo
	if truthy(os.Getenv("MANTLE_DEBUG")) {
		level = slog.LevelDebug
	}
	if err := os.MkdirAll(p.LogsDir(), 0o700); err == nil {
		f, err := os.OpenFile(filepath.Join(p.LogsDir(), strconv.Itoa(os.Getpid())+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})), func() { f.Close() }
		}
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
}

func truthy(s string) bool {
	b, err := strconv.ParseBool(s)
	return err == nil && b
}
