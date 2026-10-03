package termsetup

import "strings"

const (
	appleTerminalDomain = "com.apple.Terminal"
	appleDefaultProfile = "Basic"
	appleOptionAsMeta   = "useOptionAsMetaKey"
)

// planAppleTerminal turns on "Use Option as Meta key" for the default and startup
// profiles. Terminal.app can't send a distinct Shift+Enter, but with Option as Meta,
// Option+Enter arrives as Esc+Return, which mantle reads as a newline.
func planAppleTerminal(in Input) Proposal {
	if in.KittyKeyboard {
		return nativeProposal(in.Terminal)
	}
	base := Proposal{Kind: TargetDefaults, Domain: appleTerminalDomain}
	manual := "To change it by hand: Terminal → Settings → Profiles → Keyboard → \"Use Option as Meta key\"."
	optionNote := "Terminal.app has no separate Shift+Enter; use Option+Enter for a newline."
	if !in.Exists || isBlank(in.Content) {
		base.Status, base.Summary = Manual, "Couldn't read Terminal.app's preferences."
		base.Notes = []string{manual}
		return base
	}
	root, err := parsePlist(in.Content)
	if err != nil || root.kind != "dict" {
		base.Status, base.Summary = Manual, "Terminal.app's preferences are in an unexpected format, so mantle left them alone."
		base.Notes = []string{manual}
		return base
	}

	var names []string
	for _, key := range []string{"Default Window Settings", "Startup Window Settings"} {
		name := appleDefaultProfile
		if v := root.get(key); v != nil && v.kind == "string" && v.text != "" {
			name = v.text
		}
		if !containsString(names, name) {
			names = append(names, name)
		}
	}

	profiles := root.get("Window Settings")
	if profiles == nil || profiles.kind != "dict" {
		base.Status = Manual
		base.Summary = "Terminal.app hasn't saved any profile settings yet, so mantle left them alone."
		base.Notes = []string{manual, optionNote}
		return base
	}

	var splices []splice
	var changed, missing []string
	for _, name := range names {
		prof := profiles.get(name)
		if prof == nil || prof.kind != "dict" {
			missing = append(missing, name)
			continue
		}
		cur := prof.get(appleOptionAsMeta)
		switch {
		case cur.isTrue():
			continue
		case cur != nil:
			splices = append(splices, splice{cur.start, cur.end, "<true/>"})
		default:
			splices = append(splices, insertIntoDict(in.Content, prof, plistKey(prof.depth+1, appleOptionAsMeta, "<true/>")))
		}
		changed = append(changed, name)
	}

	missingNote := ""
	if len(missing) > 0 {
		missingNote = "Profile " + quoteList(missing) + " still uses Terminal.app's built-in defaults, so mantle didn't touch it. " + manual
	}
	if len(splices) == 0 {
		if len(missing) > 0 {
			base.Status, base.Summary = Manual, "mantle can't safely change a profile Terminal.app hasn't saved yet."
			base.Notes = []string{missingNote, optionNote}
			return base
		}
		base.Status = AlreadyInstalled
		base.Summary = "Option is already the Meta key in Terminal.app, so Option+Enter inserts a newline."
		base.Notes = []string{optionNote}
		return base
	}
	out, err := applySplices(in.Content, splices)
	if err != nil {
		base.Status, base.Summary, base.Notes = Manual, err.Error(), []string{manual}
		return base
	}
	p := editProposal(TargetDefaults, in.Path, in.Content, out,
		"Turn on \"Use Option as Meta key\" for profile "+quoteList(changed)+", so Option+Enter inserts a newline.")
	p.Domain = appleTerminalDomain
	p.Notes = []string{optionNote, "Open a new Terminal window for the change to apply."}
	if missingNote != "" {
		p.Notes = append(p.Notes, missingNote)
	}
	return p
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "\"" + n + "\""
	}
	return strings.Join(q, " and ")
}
