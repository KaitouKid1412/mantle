// The engine argv end-to-end check for plan 11's B1, against plan 02's BuildArgs.

package cli_test

import (
	"slices"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/engine"
)

func TestEngineArgv(t *testing.T) {
	res := cli.ResolverFuncs{
		ContinueFunc: func(string) (string, error) { return "11111111-1111-4111-8111-111111111111", nil },
		ResolveFunc:  func(_, arg string) (string, string, error) { return "", arg, nil },
	}
	for _, c := range []struct {
		argv   []string
		extra  []string
		prompt string
	}{
		{
			argv:   []string{"--model", "sonnet", "hello", "--add-dir", "../x"},
			extra:  []string{"--model", "sonnet", "--add-dir", "../x"},
			prompt: "hello",
		},
		{
			argv: []string{"-c", "--fork-session", "-n", "work", "--verbose", "--settings", "a.json", "--allowed-tools", "Bash(git *)", "Edit"},
			extra: []string{"-n", "work", "--resume=11111111-1111-4111-8111-111111111111", "--fork-session",
				"--settings", "a.json", "--verbose", "--allowed-tools", "Bash(git *)", "Edit"},
		},
	} {
		p, err := cli.Parse(c.argv)
		if err != nil {
			t.Fatal(err)
		}
		st, err := p.Startup("/w", res)
		if err != nil {
			t.Fatal(err)
		}
		got := engine.BuildArgs(st.Spawn)
		want := append(slices.Clone(engine.BaseArgs), c.extra...)
		if !slices.Equal(got, want) {
			t.Errorf("%q:\n got %q\nwant %q", c.argv, got, want)
		}
		if st.Prompt != c.prompt {
			t.Errorf("%q: prompt %q", c.argv, st.Prompt)
		}
	}
}
