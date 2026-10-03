//go:build deps

// Package deps pins mantle's planned dependencies so `go mod tidy` keeps them
// before any real package imports them. It is never compiled into mantle (build
// tag "deps"). Drop an entry once real code imports that module.
//
// Shared file: dependencies are added at integration windows, see CLAUDE.md.
package deps

import (
	_ "charm.land/bubbles/v2/key"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/alecthomas/chroma/v2"
	_ "github.com/aymanbagabas/go-udiff"
	_ "github.com/charmbracelet/x/ansi"
	_ "github.com/charmbracelet/x/editor"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "github.com/charmbracelet/x/vt"
	_ "github.com/creack/pty"
	_ "github.com/dustin/go-humanize"
	_ "github.com/fsnotify/fsnotify"
	_ "github.com/google/uuid"
	_ "github.com/sahilm/fuzzy"
	_ "github.com/sergi/go-diff/diffmatchpatch"
	_ "github.com/yuin/goldmark"
	_ "github.com/yuin/goldmark-emoji"
	_ "rsc.io/qr"
)
