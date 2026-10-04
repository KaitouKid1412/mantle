//go:build !no_mods

package all

// User mods: package mods blank-imports each mods/<id> from mods/link_<id>.go.
import _ "github.com/KaitouKid1412/mantle/mods"
