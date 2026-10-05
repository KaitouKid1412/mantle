package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const nextHelp = `Usage: claude [options] [command] [prompt]

Options:
  --model <model>             known
  --effort [level]            arity changed
  --zap-level <n>             new required flag
  --sparkle                   new switch
  -v, --version               known
`

func fakeProbes(t *testing.T, help string, rep EngineReport, helpErr, engErr error) {
	t.Helper()
	oldH, oldE := probeHelp, probeEngine
	probeHelp = func(context.Context, string) (string, error) { return help, helpErr }
	probeEngine = func(context.Context, string) (EngineReport, error) { return rep, engErr }
	t.Cleanup(func() { probeHelp, probeEngine = oldH, oldE })
}

func TestKnownEngineEmbedded(t *testing.T) {
	k := Known()
	if k.ClaudeVersion == "" || !slices.Contains(k.Commands, "compact") || !slices.Contains(k.Tools, "Bash") {
		t.Errorf("embedded tables %+v", k)
	}
}

func TestCheckDrift(t *testing.T) {
	fakeProbes(t, nextHelp, EngineReport{
		Commands: []EngineCommand{{Name: "compact"}, {Name: "frob"}, {Name: "wipe", Aliases: []string{"reset"}}},
		Tools:    []string{"Bash", "Zap", "mcp__gh__create"},
	}, nil, nil)
	s := CheckDrift(context.Background(), "claude", "2.1.290")
	if s.EngineVersion != "2.1.290" || s.KnownVersion != Known().ClaudeVersion {
		t.Errorf("versions %+v", s)
	}
	if !slices.Equal(s.NewCommands, []string{"frob"}) || !slices.Equal(s.NewTools, []string{"Zap"}) {
		t.Errorf("commands %q tools %q", s.NewCommands, s.NewTools)
	}
	var newFlags []string
	for _, f := range s.NewFlags {
		newFlags = append(newFlags, f.Long)
	}
	if !slices.Equal(newFlags, []string{"--zap-level", "--sparkle"}) || !slices.Equal(s.ChangedFlags, []string{"--effort"}) {
		t.Errorf("flags new %q changed %q", newFlags, s.ChangedFlags)
	}
	if got := s.Notice(); got != "Claude Code 2.1.290 adds 1 command, 1 tool and 2 flags mantle doesn't know yet; they work as engine passthrough" {
		t.Errorf("notice %q", got)
	}
	if len(s.Errors) != 0 {
		t.Errorf("errors %q", s.Errors)
	}

	// Learnt flags fix the tokenizing of their values.
	table := append(slices.Clone(Flags), s.LearntFlags()...)
	p, err := ParseWith([]string{"--zap-level", "3", "--sparkle", "hi"}, table)
	if err != nil || p.Prompt != "hi" || !slices.Equal(p.EngineArgs, []string{"--zap-level", "3", "--sparkle"}) || len(p.Warnings) != 0 {
		t.Errorf("parse %+v %v", p, err)
	}
}

func TestCheckDriftErrors(t *testing.T) {
	fakeProbes(t, "", EngineReport{}, errors.New("no help"), errors.New("no engine"))
	s := CheckDrift(context.Background(), "claude", "9.9.9")
	if len(s.Errors) != 2 || s.Notice() != "" {
		t.Errorf("state %+v", s)
	}
}

func TestNotice(t *testing.T) {
	for _, c := range []struct {
		s    DriftState
		want string
	}{
		{DriftState{EngineVersion: "1"}, ""},
		{DriftState{EngineVersion: "1", NewTools: []string{"a"}}, "Claude Code 1 adds 1 tool mantle doesn't know yet; they work as engine passthrough"},
		{DriftState{EngineVersion: "1", NewCommands: []string{"a", "b", "c"}, NewFlags: []HelpFlag{{Long: "--x"}}},
			"Claude Code 1 adds 3 commands and 1 flag mantle doesn't know yet; they work as engine passthrough"},
	} {
		if got := c.s.Notice(); got != c.want {
			t.Errorf("%+v: %q", c.s, got)
		}
	}
}

func TestLearntFlagsSkipKnown(t *testing.T) {
	s := DriftState{NewFlags: []HelpFlag{{Long: "--model", Arity: "required"}, {Long: "--new", Short: "-c", Arity: "optional"}, {Long: "-z"}}}
	got := s.LearntFlags()
	if len(got) != 1 || got[0].Long != "--new" || got[0].Short != "" || got[0].Arity != ArityOptional || got[0].Class != Forward {
		t.Errorf("learnt %+v", got)
	}
}

func TestRuntimeDrift(t *testing.T) {
	fakeClaude(t) // answers --version with 9.9.9
	fakeProbes(t, nextHelp, EngineReport{Commands: []EngineCommand{{Name: "frob"}}}, nil, nil)
	path := filepath.Join(t.TempDir(), "state", "drift.json")
	ctx := context.Background()

	s, ran, err := RuntimeDrift(ctx, path)
	if err != nil || !ran || s.EngineVersion != "9.9.9" || !slices.Equal(s.NewCommands, []string{"frob"}) {
		t.Fatalf("first: %+v %v %v", s, ran, err)
	}
	saved, ok := LoadDriftState(path)
	if !ok || saved.EngineVersion != "9.9.9" || len(saved.NewFlags) != 2 {
		t.Fatalf("saved %+v", saved)
	}
	// Same engine version: no new check.
	fakeProbes(t, "", EngineReport{}, errors.New("must not run"), errors.New("must not run"))
	if s, ran, err := RuntimeDrift(ctx, path); err != nil || ran || s.EngineVersion != "9.9.9" {
		t.Errorf("second: %+v %v %v", s, ran, err)
	}
	t.Setenv(EnvDriftCheck, "off")
	if _, ran, _ := RuntimeDrift(ctx, filepath.Join(t.TempDir(), "x.json")); ran {
		t.Error("ran with the check off")
	}
}

func TestDispatchUsesLearntFlags(t *testing.T) {
	fakeClaude(t) // also isolates MANTLE_HOME
	s := DriftState{EngineVersion: "9.9.9", NewFlags: []HelpFlag{{Long: "--zap-level", Arity: "required"}}}
	if err := SaveDriftState(DriftStatePath(), s); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	p, done, _ := Dispatch(context.Background(), []string{"--zap-level", "3", "hi"}, &out, &errb)
	if done || p.Prompt != "hi" || len(p.Unknown) != 0 || !strings.Contains(strings.Join(p.EngineArgs, " "), "--zap-level 3") {
		t.Errorf("dispatch %+v done %v %s", p, done, errb.String())
	}
}
