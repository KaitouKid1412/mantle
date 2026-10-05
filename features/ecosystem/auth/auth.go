// Package auth implements /login and /logout. Both are interactive flows in
// Claude Code, refused headlessly, so mantle runs `claude auth login|logout`
// with the terminal attached, then restarts the engine on the same session so
// it uses the new credentials.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package auth

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.auth"

// Dialog IDs.
const (
	LoginDialogID  = "dialog.ecosystem.login"
	LogoutDialogID = "dialog.ecosystem.logout"
)

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 350, Parity: []string{"EC-17", "EC-18", "EC-46"}, Setup: Setup})
}

// Setup registers /login and /logout.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "login", Source: ext.SourceBuiltin, Description: "Sign in to your Anthropic account",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(LoginDialogID, nil) },
	})
	r.AddCommand(ext.Command{
		Name: "logout", Source: ext.SourceBuiltin, Description: "Sign out of your Anthropic account",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(LogoutDialogID, nil) },
	})
	r.AddDialog(LoginDialogID, eco.Factory(NewLogin))
	r.AddDialog(LogoutDialogID, eco.Factory(NewLogout))
	r.AddStory(ext.Story{ID: "ecosystem.auth/login", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := NewLogin(ctx, nil)
		d.Root().(*loginView).setAccount(claudecli.AuthInfo{LoggedIn: true, AuthMethod: "claude.ai",
			Email: "user@example.test", SubscriptionType: "max"})
		return d.View(ctx, a)
	}})
	r.AddStory(ext.Story{ID: "ecosystem.auth/logout", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := NewLogout(ctx, nil)
		return d.View(ctx, a)
	}})
	return nil
}

// Describe is a one-line description of an account.
func Describe(a claudecli.AuthInfo) string {
	if !a.LoggedIn {
		return "Not signed in."
	}
	who := a.Email
	if who == "" {
		who = a.OrgName
	}
	if who == "" {
		who = "an account"
	}
	s := "Signed in as " + who
	switch {
	case a.SubscriptionType != "":
		s += fmt.Sprintf(" (%s plan)", a.SubscriptionType)
	case a.AuthMethod != "":
		s += fmt.Sprintf(" (%s)", a.AuthMethod)
	}
	return s + "."
}

// statusCmd reads `claude auth status` off the UI goroutine.
func statusCmd(ctx ext.Ctx, key string) tea.Cmd {
	cli := eco.CLI(ctx)
	return eco.Async(key, func() (any, error) { return cli.AuthStatus(eco.Background()) })
}

// ---- /login ----

type method struct {
	m      claudecli.AuthMethod
	label  string
	detail string
}

var methods = []method{
	{claudecli.AuthClaudeAI, "Claude subscription", "Pro, Max, Team or Enterprise"},
	{claudecli.AuthConsole, "Anthropic Console", "API usage billing"},
	{claudecli.AuthSSO, "Single sign-on", "your organization's identity provider"},
}

// NewLogin builds the /login dialog: pick a method, then sign in in the terminal.
func NewLogin(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	v := &loginView{}
	items := make([]eco.MenuItem, len(methods))
	for i, m := range methods {
		m := m
		items[i] = eco.MenuItem{Label: m.label, Detail: m.detail, Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			cmd, err := eco.CLI(ctx).AuthLoginCmd(m.m, "")
			if err != nil {
				return eco.ExecErr("auth.login", err)
			}
			return eco.Exec("auth.login", cmd)
		}}
	}
	v.MenuView = eco.NewMenu("", []string{"Checking your account…"}, items...)
	d := eco.NewDialog(LoginDialogID, "Sign in", v)
	d.CloseLine = "Sign-in cancelled"
	return d, nil
}

type loginView struct{ *eco.MenuView }

func (v *loginView) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return statusCmd(ctx, "auth.status") }

func (v *loginView) setAccount(a claudecli.AuthInfo) {
	v.Intro = []string{Describe(a), "", "Choose how to sign in:"}
}

func (v *loginView) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ResultMsg:
		switch m.Key {
		case "auth.status":
			if a, ok := m.Value.(claudecli.AuthInfo); ok && m.Err == nil {
				v.setAccount(a)
			} else {
				v.Intro = []string{"Choose how to sign in:"}
			}
			ctx.Invalidate(d.ID())
		case "auth.after-login":
			a, _ := m.Value.(claudecli.AuthInfo)
			if m.Err != nil || !a.LoggedIn {
				d.SetStatus(ctx, "Sign-in didn't finish. Try again, or press esc.", theme.Warning)
				return nil
			}
			d.CloseLine = "" // the notice shows the new account
			return tea.Batch(d.Close(ctx),
				ctx.Notify(ext.Notice{Key: "auth.login", Level: ext.NoticeSuccess, Text: Describe(a), Source: FeatureID}),
				eco.RestartEngine(ctx, "to use the new login"))
		}
	case eco.ExecDoneMsg:
		if m.Key != "auth.login" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		eco.Busy(ctx, d, "Checking the new login")
		return statusCmd(ctx, "auth.after-login")
	}
	return nil
}

// ---- /logout ----

// NewLogout builds the /logout confirmation.
func NewLogout(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	v := &logoutView{ConfirmView: &eco.ConfirmView{
		Question: []string{"Sign out of Claude Code?", "",
			"This signs out every Claude Code session on this machine, not just mantle."},
		YesLabel: "Sign out", Danger: true,
	}}
	d := eco.NewDialog(LogoutDialogID, "Sign out", v)
	d.CloseLine = "Sign-out cancelled"
	v.OnYes = func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
		cmd, err := eco.CLI(ctx).AuthLogoutCmd()
		if err != nil {
			return eco.ExecErr("auth.logout", err)
		}
		return eco.Exec("auth.logout", cmd)
	}
	return d, nil
}

type logoutView struct{ *eco.ConfirmView }

// accept runs OnYes without popping: the confirm is the dialog's root.
func (v *logoutView) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if a == ext.ActConfirmYes && v.Choice() {
		return true, v.OnYes(ctx, d)
	}
	return v.ConfirmView.Action(ctx, d, a)
}

func (v *logoutView) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() == "y" || k.String() == "Y" {
		return true, v.OnYes(ctx, d)
	}
	return v.ConfirmView.Key(ctx, d, k)
}

func (v *logoutView) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	m, ok := msg.(eco.ExecDoneMsg)
	if !ok || m.Key != "auth.logout" {
		return nil
	}
	if m.Err != nil {
		eco.Fail(ctx, d, m.Err)
		return nil
	}
	d.CloseLine = ""
	return tea.Batch(d.Close(ctx),
		ctx.Notify(ext.Notice{Key: "auth.logout", Level: ext.NoticeInfo, Text: "Signed out. Use /login to sign in again.", Source: FeatureID}),
		eco.RestartEngine(ctx, "after signing out"))
}
