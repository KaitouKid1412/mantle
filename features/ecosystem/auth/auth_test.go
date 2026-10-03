package auth

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/claudecli/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLogin(t *testing.T) {
	ecotest.FakeClaude(t, map[string]string{"auth status": fixture(t, "auth-status.json")})
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, t.TempDir())
	x := ecotest.Capture(t)
	d, _ := NewLogin(ctx, nil)
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 80); !strings.Contains(s, "Signed in as user@example.test (max plan)") ||
		!strings.Contains(s, "Anthropic Console") {
		t.Fatalf("screen = %q", s)
	}
	ecotest.Press(t, ctx, d, "down", "enter")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"auth", "login", "--console"}) {
		t.Fatalf("exec = %q", got)
	}
	if !d.Closed() || len(eng.Restarts) != 1 {
		t.Errorf("after login: closed=%v restarts=%d", d.Closed(), len(eng.Restarts))
	}
	found := false
	for _, n := range ctx.Notices {
		found = found || (n.Key == "auth.login" && strings.Contains(n.Text, "user@example.test"))
	}
	if !found {
		t.Errorf("no sign-in notice: %+v", ctx.Notices)
	}
}

func TestLoginCancelled(t *testing.T) {
	ecotest.FakeClaude(t, map[string]string{"auth status": fixture(t, "auth-status-loggedout.json")})
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, t.TempDir())
	x := ecotest.Capture(t)
	d, _ := NewLogin(ctx, nil)
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 80); !strings.Contains(s, "Not signed in") {
		t.Fatalf("screen = %q", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	if d.Closed() || len(eng.Restarts) != 0 || !strings.Contains(ecotest.Screen(ctx, d, 80), "didn't finish") {
		t.Errorf("a login that didn't sign in keeps the dialog open")
	}
	x.Err = errors.New("exit status 1")
	ecotest.Press(t, ctx, d, "3")
	if !strings.Contains(ecotest.Screen(ctx, d, 80), "exit status 1") {
		t.Errorf("exec error not shown")
	}
	if got := x.Args(); !reflect.DeepEqual(got[1], []string{"auth", "login", "--sso"}) {
		t.Errorf("exec = %q", got)
	}
}

func TestLogout(t *testing.T) {
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, "/work")
	x := ecotest.Capture(t)
	d, _ := NewLogout(ctx, nil)
	ecotest.Press(t, ctx, d, "enter") // default is No
	if !d.Closed() || len(x.Cmds) != 0 {
		t.Fatalf("enter on No closes without signing out: closed=%v execs=%d", d.Closed(), len(x.Cmds))
	}
	d, _ = NewLogout(ctx, nil)
	ecotest.Press(t, ctx, d, "y")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"auth", "logout"}) {
		t.Fatalf("exec = %q", got)
	}
	if !d.Closed() || len(eng.Restarts) != 1 {
		t.Errorf("after logout: closed=%v restarts=%d", d.Closed(), len(eng.Restarts))
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]claudecli.AuthInfo{
		"Not signed in.":                    {},
		"Signed in as Org (console).":       {LoggedIn: true, OrgName: "Org", AuthMethod: "console"},
		"Signed in as an account.":          {LoggedIn: true},
		"Signed in as a@b.test (pro plan).": {LoggedIn: true, Email: "a@b.test", SubscriptionType: "pro"},
	}
	for want, a := range cases {
		if got := Describe(a); got != want {
			t.Errorf("Describe(%+v) = %q, want %q", a, got, want)
		}
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, n := range []string{"login", "logout"} {
		if _, ok := r.Command(n); !ok {
			t.Errorf("no /%s", n)
		}
	}
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) { testkit.RunStory(t, s) })
	}
}
