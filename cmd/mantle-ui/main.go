// Command mantle-ui is the mantle terminal UI.
//
// Primary owner: plan 01.
package main

import (
	"fmt"
	"os"

	_ "github.com/KaitouKid1412/mantle/features/all"
)

func main() {
	fmt.Fprintln(os.Stderr, "mantle-ui: not implemented yet (see docs/plans/)")
	os.Exit(1)
}
