// Command mantle is the supervising launcher. It imports only the standard
// library (plus internal/launcher, which is also stdlib-only). See
// internal/launcher.
//
// Primary owner: plan 10.
package main

import (
	"os"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

func main() {
	os.Exit(launcher.Main(os.Args[1:]))
}
