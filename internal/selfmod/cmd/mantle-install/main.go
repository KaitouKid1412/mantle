// Command mantle-install installs mantle from a dev checkout into ~/.mantle.
// It is run by scripts/install.sh (make install):
//
//	go run ./internal/selfmod/cmd/mantle-install [-branch main] [-link-dir ~/.local/bin]
//
// Re-running it is idempotent.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/internal/selfmod"
)

func main() {
	home, _ := os.UserHomeDir()
	branch := flag.String("branch", "main", "upstream branch of the dev checkout")
	linkDir := flag.String("link-dir", filepath.Join(home, ".local", "bin"), `directory for the mantle symlink ("" to skip)`)
	devRepo := flag.String("dev-repo", "", "mantle checkout to install from (default: the current git checkout)")
	flag.Parse()

	if *devRepo == "" {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			fail(fmt.Errorf("run this from a mantle checkout (git rev-parse: %v)", err))
		}
		*devRepo = strings.TrimSpace(string(out))
	}
	l, err := launcher.DefaultLayout()
	if err != nil {
		fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res, err := selfmod.Install(ctx, selfmod.InstallOptions{
		Layout:  l,
		DevRepo: *devRepo,
		Branch:  *branch,
		LinkDir: *linkDir,
		Out:     os.Stdout,
	})
	if err != nil {
		fail(err)
	}
	fmt.Printf("\nmantle is installed (build %s). Run `mantle` to start it.\n", res.BuildID)
	if res.Behind {
		fmt.Println("Your mods are on an older upstream: run /mantle update inside mantle to rebase them.")
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "mantle-install: %v\n", err)
	os.Exit(1)
}
