// Package mods holds user mods created by /mantle, one subpackage per mod.
//
// A mod lives in mods/<mod-id>/ and registers an ext.Feature with
// Order >= ext.ModOrder from its init function. This package links the mods
// into mantle-ui: each mod adds one file, mods/link_<mod-id>.go, that
// blank-imports its package. One file per mod keeps mods independent, so
// undo and update never conflict here. See docs/EXTENDING.md.
//
// Primary owner: plan 10 (docs/plans/10-selfmod-launcher.md).
package mods
