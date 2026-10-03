package termsetup

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	iTermDomain = "com.googlecode.iterm2"
	// iTerm2 names key bindings "0x<char>-0x<modifier flags>-0x<key code>":
	// Return (0xd), Shift (NSEventModifierFlagShift), key code 0x24.
	iTermShiftReturnKey = "0xd-0x20000-0x24"
	iTermActionEscape   = 10 // send Esc + text
	iTermActionHex      = 11 // send hex codes
	iTermActionText     = 12 // send text
	iTermHexNewline     = "0x1b 0x0d"
)

const (
	modShift   = 0x20000
	modControl = 0x40000
	modOption  = 0x80000
	modCommand = 0x100000
)

func planITerm2(in Input) Proposal {
	if in.KittyKeyboard {
		return nativeProposal(in.Terminal)
	}
	manual := []string{
		"To add it by hand: iTerm2 → Settings → Keys → Key Bindings → +, press Shift+Return, " +
			"choose \"Send Hex Codes\" and enter " + iTermHexNewline + ".",
	}
	if !in.Exists || isBlank(in.Content) {
		return Proposal{Status: Manual, Kind: TargetDefaults, Domain: iTermDomain,
			Summary: "Couldn't read iTerm2's preferences.", Notes: manual}
	}
	root, err := parsePlist(in.Content)
	if err != nil || root.kind != "dict" {
		return Proposal{Status: Manual, Kind: TargetDefaults, Domain: iTermDomain,
			Summary: "iTerm2's preferences are in an unexpected format, so mantle left them alone.", Notes: manual}
	}
	km := root.get("GlobalKeyMap")
	if km != nil && km.kind != "dict" {
		return Proposal{Status: Manual, Kind: TargetDefaults, Domain: iTermDomain,
			Summary: "iTerm2's global key map is in an unexpected format, so mantle left it alone.", Notes: manual}
	}
	if km != nil {
		for i, k := range km.keys {
			if !isShiftReturn(k) {
				continue
			}
			v := km.vals[i]
			if iTermSendsNewline(v) {
				return Proposal{Status: AlreadyInstalled, Kind: TargetDefaults, Domain: iTermDomain,
					Summary: "iTerm2 already has a Shift+Enter binding that inserts a newline."}
			}
			return Proposal{Status: Manual, Kind: TargetDefaults, Domain: iTermDomain,
				Summary: "iTerm2 already binds Shift+Enter to something else (" + describeITermAction(v) + "), so mantle left it alone.",
				Notes:   append([]string{"Remove or change that binding in iTerm2 → Settings → Keys → Key Bindings, then run /terminal-setup again."}, manual...)}
		}
	}

	pairs := [][2]string{
		{"Action", "<integer>" + strconv.Itoa(iTermActionHex) + "</integer>"},
		{"Text", "<string>" + iTermHexNewline + "</string>"},
	}
	var sp splice
	if km == nil {
		entry := indent(root.depth+1) + "<key>GlobalKeyMap</key>\n" +
			indent(root.depth+1) + "<dict>\n" +
			plistDictEntry(root.depth+2, iTermShiftReturnKey, pairs) +
			indent(root.depth+1) + "</dict>\n"
		sp = insertIntoDict(in.Content, root, entry)
	} else {
		sp = insertIntoDict(in.Content, km, plistDictEntry(km.depth+1, iTermShiftReturnKey, pairs))
	}
	out, err := applySplices(in.Content, []splice{sp})
	if err != nil {
		return Proposal{Status: Manual, Kind: TargetDefaults, Domain: iTermDomain, Summary: err.Error(), Notes: manual}
	}
	p := editProposal(TargetDefaults, in.Path, in.Content, out,
		"Add a global iTerm2 key binding: Shift+Enter sends Esc+Return, which mantle reads as a newline.")
	p.Domain = iTermDomain
	p.Notes = []string{
		"Quit and reopen iTerm2 so it loads the new binding.",
		"A profile with its own Shift+Return mapping still takes precedence over this global one.",
	}
	return p
}

// isShiftReturn reports whether an iTerm2 key-map entry name means Shift+Return with no
// other modifiers.
func isShiftReturn(key string) bool {
	parts := strings.Split(key, "-")
	if len(parts) < 2 {
		return false
	}
	ch, err1 := strconv.ParseUint(strings.TrimPrefix(parts[0], "0x"), 16, 32)
	mods, err2 := strconv.ParseUint(strings.TrimPrefix(parts[1], "0x"), 16, 64)
	if err1 != nil || err2 != nil || ch != 0xd {
		return false
	}
	return mods&modShift != 0 && mods&(modControl|modOption|modCommand) == 0
}

func iTermAction(v *plistNode) (action int, text string, ok bool) {
	if v == nil || v.kind != "dict" {
		return 0, "", false
	}
	a := v.get("Action")
	if a == nil || a.kind != "integer" {
		return 0, "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(a.text))
	if err != nil {
		return 0, "", false
	}
	if t := v.get("Text"); t != nil {
		text = t.text
	}
	return n, text, true
}

// iTermSendsNewline reports whether a key-map entry sends something mantle treats as a
// newline: Esc+Return or a line feed.
func iTermSendsNewline(v *plistNode) bool {
	action, text, ok := iTermAction(v)
	if !ok {
		return false
	}
	switch action {
	case iTermActionHex:
		b, ok := parseHexCodes(text)
		return ok && (b == NewlineSeq || b == "\n")
	case iTermActionText:
		return text == `\n` || text == "\n"
	case iTermActionEscape:
		return text == "\r" || text == `\r`
	}
	return false
}

func parseHexCodes(s string) (string, bool) {
	var b strings.Builder
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(f), "0x"), 16, 8)
		if err != nil {
			return "", false
		}
		b.WriteByte(byte(n))
	}
	return b.String(), b.Len() > 0
}

func describeITermAction(v *plistNode) string {
	action, text, ok := iTermAction(v)
	if !ok {
		return "an unrecognized action"
	}
	switch action {
	case iTermActionHex:
		return "sends hex codes " + text
	case iTermActionText:
		return fmt.Sprintf("sends the text %q", text)
	case iTermActionEscape:
		return fmt.Sprintf("sends Esc + %q", text)
	}
	return fmt.Sprintf("iTerm2 action %d", action)
}
