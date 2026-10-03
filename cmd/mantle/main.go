// Command mantle is the supervising launcher (stdlib only, never rebuilt by /mantle).
//
// Primary owner: plan 10.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mantle: not implemented yet (see docs/plans/)")
	os.Exit(1)
}
