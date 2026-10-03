package turn_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/engine/enginetest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// The scripted engine asks for a permission (always-allow suggestion), an
// AskUserQuestion and an elicitation, and checks the exact control_response each
// dialog sends. Its expects fail the run if mantle answers anything else.
const e2eScript = `
{"on": {"type":"control_request","request":{"subtype":"list_models"}}, "respond": {"models":[{"value":"default","displayName":"Default","supportsAutoMode":false}]}}
{"on": {"type":"control_request","request":{"subtype":"set_permission_mode"}}, "respond": {"mode":"default"}}
{"delay": 50}
{"request": {"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls -la"},"tool_use_id":"toolu_1","permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls:*"}],"behavior":"allow","destination":"localSettings"}]}, "id": "cli_1"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_1","response":{"behavior":"allow","toolUseID":"toolu_1","decisionClassification":"user_permanent","updatedPermissions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls:*"}],"behavior":"allow","destination":"localSettings"}]}}}, "timeout": 10000}
{"request": {"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"toolu_2","input":{"questions":[{"question":"Which runner?","header":"Runner","multiSelect":false,"options":[{"label":"make","description":""},{"label":"go test","description":""}]}]}}, "id": "cli_2"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_2","response":{"behavior":"allow","updatedInput":{"answers":{"Which runner?":"go test"}}}}}, "timeout": 10000}
{"request": {"subtype":"elicitation","mcp_server_name":"tickets","message":"Ticket title?","requested_schema":{"type":"object","properties":{"title":{"type":"string"}}}}, "id": "cli_3"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_3","response":{"action":"cancel"}}}, "timeout": 10000}
`

func TestEndToEndPromptsThroughRealEngine(t *testing.T) {
	hostEnv(t)
	script := enginefake.MustParse(e2eScript)
	script.Rules = append(script.Rules, enginefake.InitializeRule(nil))
	sp := &enginetest.Spawner{Scripts: []*enginefake.Script{script}}

	var prog atomic.Pointer[tea.Program]
	m := engine.NewManager(func(msg tea.Msg) {
		if p := prog.Load(); p != nil {
			p.Send(msg)
		}
	})
	m.Spawner, m.RunDir, m.Binary = sp, "-", "claude"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Close(ctx)
	})

	proj := projectDir(t)
	root := app.New(app.Options{
		Host:              app.NewHost(turnFeature(t), app.HostOptions{Core: app.CoreFeatures()}),
		NoBackgroundQuery: true,
		Session:           ext.SessionInfo{EngineID: ext.MainEngine, Cwd: proj},
		MainSpawn:         &ext.SpawnOpts{Cwd: proj},
		Spawn: func(id string, o ext.SpawnOpts) error {
			_, err := m.Start(id, o)
			return err
		},
	})
	hn := testkit.New(t, root, testkit.WithSize(100, 30))
	prog.Store(hn.Program())

	hn.WaitForText("Do you trust this folder?", 5*time.Second)
	hn.Send("enter")

	hn.WaitForText("ls -la", 10*time.Second)
	hn.Send("2") // Yes, and don't ask again for Bash(ls:*)

	hn.WaitForText("Which runner?", 10*time.Second)
	hn.Send("2") // go test

	hn.WaitForText("Ticket title?", 10*time.Second)
	hn.Send("esc") // cancel

	procs := sp.Procs()
	if len(procs) != 1 {
		t.Fatalf("spawns = %d", len(procs))
	}
	done := make(chan struct{})
	go func() {
		// The script has no steps left once the last expect matched; close stdin.
		time.Sleep(200 * time.Millisecond)
		_ = procs[0].Stdin.Close()
		close(done)
	}()
	<-done
	code, err := procs[0].Wait()
	if err != nil || code != 0 {
		t.Fatalf("scripted engine: code=%d err=%v", code, err)
	}
	hn.Settle(100*time.Millisecond, time.Second)
	if _, err := hn.Quit(); err != nil {
		t.Fatal(err)
	}
}
