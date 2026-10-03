//go:build clisync

// This test needs plan 11's internal/cli. It is behind the clisync build tag
// until internal/cli is merged into this branch; then the tag goes away.
// Run it with: go test -tags clisync ./internal/selfmod -run CLISync

package selfmod

import (
	"slices"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// TestCLISyncPassthroughList checks the launcher's copy of the passthrough
// subcommands against the canonical table. The launcher cannot import
// internal/cli itself (it is standard-library-only), so the check lives here.
func TestCLISyncPassthroughList(t *testing.T) {
	want := slices.Clone(cli.PassthroughSubcommands())
	got := slices.Clone(launcher.PassthroughSubcommands)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("launcher.PassthroughSubcommands drifted from cli.PassthroughSubcommands():\n got %q\nwant %q", got, want)
	}
}
