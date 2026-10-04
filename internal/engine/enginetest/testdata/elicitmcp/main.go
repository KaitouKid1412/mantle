// Command elicitmcp is a minimal stdio MCP server for tests: its one tool,
// ask_name, asks the user for a name through an elicitation request and returns it.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

var (
	outMu   sync.Mutex
	waiting = map[string]chan msg{}
	waitMu  sync.Mutex
)

func send(v any) {
	b, _ := json.Marshal(v)
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.Write(append(b, '\n'))
}

func reply(id json.RawMessage, result any) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m msg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" { // a response to one of our requests
			waitMu.Lock()
			ch := waiting[string(m.ID)]
			waitMu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(m.Params, &p)
			reply(m.ID, map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "elicitmcp", "version": "1.0.0"},
			})
		case "tools/list":
			reply(m.ID, map[string]any{"tools": []any{map[string]any{
				"name": "ask_name", "description": "Ask the user for their name",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}})
		case "tools/call":
			go askName(m.ID)
		case "ping":
			reply(m.ID, map[string]any{})
		default:
			if len(m.ID) > 0 {
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			}
		}
	}
}

func askName(callID json.RawMessage) {
	id := `"elicit-1"`
	ch := make(chan msg, 1)
	waitMu.Lock()
	waiting[id] = ch
	waitMu.Unlock()
	send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "elicitation/create", "params": map[string]any{
		"message": "What is your name?",
		"requestedSchema": map[string]any{"type": "object", "required": []string{"name"},
			"properties": map[string]any{"name": map[string]any{"type": "string", "title": "Name"}}},
	}})
	res := <-ch
	var r struct {
		Action  string         `json:"action"`
		Content map[string]any `json:"content"`
	}
	_ = json.Unmarshal(res.Result, &r)
	text := fmt.Sprintf("action=%s name=%v", r.Action, r.Content["name"])
	reply(callID, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
}
