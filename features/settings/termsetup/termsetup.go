// Package termsetup plans the terminal changes behind /terminal-setup: making a key
// combination insert a newline in the prompt instead of submitting it.
//
// Everything here except Apply is pure. A planner takes the terminal, the current
// contents of its config (a file, or an exported plist domain) and what the running
// terminal told Bubble Tea about its keyboard support, and returns a Proposal: either
// "nothing to do" with an explanation, or the new contents plus a unified diff the user
// confirms before Apply writes it (after taking a backup).
package termsetup

import (
	"fmt"

	udiff "github.com/aymanbagabas/go-udiff"
)

// NewlineSeq is what installed key bindings send: ESC followed by CR, which terminals
// report as alt+enter and which mantle's editor treats as "insert newline".
const NewlineSeq = "\x1b\r"

// Terminal identifies a terminal emulator.
type Terminal string

const (
	TerminalUnknown Terminal = ""
	Ghostty         Terminal = "ghostty"
	Kitty           Terminal = "kitty"
	WezTerm         Terminal = "wezterm"
	ITerm2          Terminal = "iterm2"
	AppleTerminal   Terminal = "apple-terminal"
	VSCode          Terminal = "vscode"
	VSCodeInsiders  Terminal = "vscode-insiders"
	VSCodium        Terminal = "vscodium"
	Cursor          Terminal = "cursor"
	Windsurf        Terminal = "windsurf"
	Alacritty       Terminal = "alacritty"
)

// Terminals lists every terminal with an installer, in display order.
var Terminals = []Terminal{Ghostty, Kitty, WezTerm, ITerm2, AppleTerminal, VSCode, VSCodeInsiders, VSCodium, Cursor, Windsurf, Alacritty}

// Name is the terminal's display name.
func (t Terminal) Name() string {
	switch t {
	case Ghostty:
		return "Ghostty"
	case Kitty:
		return "kitty"
	case WezTerm:
		return "WezTerm"
	case ITerm2:
		return "iTerm2"
	case AppleTerminal:
		return "Terminal.app"
	case VSCode:
		return "VS Code"
	case VSCodeInsiders:
		return "VS Code Insiders"
	case VSCodium:
		return "VSCodium"
	case Cursor:
		return "Cursor"
	case Windsurf:
		return "Windsurf"
	case Alacritty:
		return "Alacritty"
	}
	return "this terminal"
}

// Status says what a Proposal asks of the user.
type Status int

const (
	// Native: the terminal already reports Shift+Enter distinctly; nothing to install.
	Native Status = iota
	// AlreadyInstalled: a matching binding is already configured.
	AlreadyInstalled
	// Edit: Proposal.New holds the edited config; confirm, then Apply.
	Edit
	// Manual: mantle won't edit safely (conflict, unsupported format); Notes explain.
	Manual
	// Unsupported: no installer for this terminal.
	Unsupported
)

func (s Status) String() string {
	switch s {
	case Native:
		return "native"
	case AlreadyInstalled:
		return "already-installed"
	case Edit:
		return "edit"
	case Manual:
		return "manual"
	case Unsupported:
		return "unsupported"
	}
	return fmt.Sprintf("status(%d)", int(s))
}

// TargetKind says how Apply reaches the config.
type TargetKind int

const (
	// TargetNone: nothing to write.
	TargetNone TargetKind = iota
	// TargetFile: a plain file at Proposal.Path.
	TargetFile
	// TargetDefaults: a macOS preferences domain (Proposal.Domain), exported and
	// imported as an XML plist with the `defaults` tool.
	TargetDefaults
)

// Input is everything a planner looks at.
type Input struct {
	Terminal Terminal
	// Path is the config file (TargetFile) or a display label for the domain.
	Path string
	// Exists reports whether the config was found. Content is its bytes (for a
	// defaults domain: the `defaults export <domain> -` output).
	Exists  bool
	Content []byte
	// KittyKeyboard is true when the running terminal confirmed the kitty keyboard
	// protocol (Bubble Tea's KeyboardEnhancementsMsg reported key disambiguation).
	KittyKeyboard bool
	// InTmux is true when mantle runs inside tmux.
	InTmux bool
	// LegacyPath names an old-format config found instead of the current one
	// (Alacritty's alacritty.yml), if any.
	LegacyPath string
}

// Proposal is a planner's answer.
type Proposal struct {
	Terminal Terminal
	Status   Status
	Kind     TargetKind
	Path     string // file path, or the domain's display label
	Domain   string // defaults domain for TargetDefaults
	Old, New []byte // only for Status == Edit
	Diff     string // unified diff of Old → New
	Summary  string // one line for the dialog
	Notes    []string
}

// Plan dispatches to the terminal's planner and appends terminal-independent notes.
func Plan(in Input) Proposal {
	var p Proposal
	switch in.Terminal {
	case Ghostty, Kitty:
		p = planKittyProtocol(in)
	case WezTerm:
		p = planWezTerm(in)
	case ITerm2:
		p = planITerm2(in)
	case AppleTerminal:
		p = planAppleTerminal(in)
	case VSCode, VSCodeInsiders, VSCodium, Cursor, Windsurf:
		p = planVSCode(in)
	case Alacritty:
		p = planAlacritty(in)
	default:
		p = planUnknown(in)
	}
	p.Terminal = in.Terminal
	if p.Path == "" {
		p.Path = in.Path
	}
	if in.InTmux {
		p.Notes = append(p.Notes, tmuxNote)
	}
	return p
}

const tmuxNote = "Inside tmux, modified keys only reach mantle when tmux forwards them: " +
	"add `set -s extended-keys on` and `set -s extended-keys-format csi-u` (tmux 3.5 or newer) to ~/.tmux.conf."

const fallbackNote = "Without a binding you can still insert a newline with ctrl+j, or by typing \\ and then Enter."

func planUnknown(in Input) Proposal {
	return Proposal{
		Status:  Unsupported,
		Summary: "mantle doesn't know how to configure this terminal.",
		Notes: []string{
			"Terminals that support the kitty keyboard protocol (Ghostty, kitty, WezTerm, recent iTerm2 and Alacritty) report Shift+Enter without any setup.",
			fallbackNote,
		},
	}
}

// editProposal fills an Edit proposal with its diff.
func editProposal(kind TargetKind, label string, old, new []byte, summary string) Proposal {
	return Proposal{
		Status:  Edit,
		Kind:    kind,
		Path:    label,
		Old:     old,
		New:     new,
		Diff:    unifiedDiff(label, old, new),
		Summary: summary,
	}
}

func unifiedDiff(label string, old, new []byte) string {
	return udiff.Unified(label+" (current)", label+" (proposed)", string(old), string(new))
}
