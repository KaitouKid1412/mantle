# Parity evidence for rows no registration names

`make parity` marks a docs/PARITY.md row **evidenced** when one of the files in this
directory lists it. Use these files only for rows a `Parity:` tag can't carry: engine
plumbing, libraries, launcher internals, tooling. Point at the tests (package and test
names) or the code that cover the row; for engine passthrough rows (code E), name the
scenario or test that exercises them through mantle. Features with a registration are
tagged there instead.

Each owner keeps its rows in `NN.md` (its plan number), so plans never edit the same
file. One table row per PARITY row, for example:

    | ID | Evidence |
    |---|---|
    | XX-01 | `internal/pkg` TestSomething (what it checks) |

This README is not read for evidence.
