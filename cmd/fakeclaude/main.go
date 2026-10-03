// Command fakeclaude is a scripted stand-in for the claude binary, for tests.
//
// Primary owner: plan 02.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fakeclaude: not implemented yet (see docs/plans/)")
	os.Exit(1)
}
