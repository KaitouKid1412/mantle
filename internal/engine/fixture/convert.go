package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// ScriptOptions tunes Script.
type ScriptOptions struct {
	// KeepTiming adds delay steps for gaps between engine lines longer than this
	// many ms (0 = no delays).
	KeepTiming int64
}

// Script turns a recording into a fakeclaude script that replays the engine side
// against a new client:
//
//   - the client's control requests become standing rules answering each subtype with
//     the first recorded response, whenever they arrive;
//   - the client's user messages and its answers to engine requests become expect
//     steps, in order;
//   - engine output becomes emit steps; prompt uuids the client sent are substituted
//     with the new client's uuids.
func Script(entries []Entry, o ScriptOptions) (*enginefake.Script, error) {
	type head struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		UUID      string          `json:"uuid"`
		Request   json.RawMessage `json:"request"`
		Response  struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	s := &enginefake.Script{}
	clientReq := map[string]string{} // client request id -> subtype
	ruled := map[string]bool{}       // subtypes that already have a rule
	var uuids [][2]string            // recorded uuid -> var name
	var lastT int64 = -1

	for i, e := range entries {
		var h head
		if err := json.Unmarshal(e.Msg, &h); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		switch e.Dir {
		case In:
			switch h.Type {
			case "control_request":
				var sub struct {
					Subtype string `json:"subtype"`
				}
				_ = json.Unmarshal(h.Request, &sub)
				clientReq[h.RequestID] = sub.Subtype
			case "user":
				name := fmt.Sprintf("u%d", len(uuids)+1)
				st := enginefake.Step{Expect: json.RawMessage(`{"type":"user"}`)}
				if h.UUID != "" {
					st.Save = map[string]string{name: "uuid"}
					uuids = append(uuids, [2]string{h.UUID, name})
				}
				s.Steps = append(s.Steps, st)
			case "control_response":
				pat, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]string{"request_id": h.Response.RequestID}})
				s.Steps = append(s.Steps, enginefake.Step{Expect: pat})
			}
		case Out:
			if h.Type == "keep_alive" {
				continue
			}
			if h.Type == "control_response" {
				if sub, ok := clientReq[h.Response.RequestID]; ok {
					if !ruled[sub] {
						ruled[sub] = true
						s.Rules = append(s.Rules, ruleFor(sub, e.Msg))
					}
					continue
				}
			}
			if o.KeepTiming > 0 && lastT >= 0 && e.T-lastT > o.KeepTiming {
				s.Steps = append(s.Steps, enginefake.Step{Delay: int(e.T - lastT)})
			}
			lastT = e.T
			msg := e.Msg
			for _, u := range uuids {
				msg = bytes.ReplaceAll(msg, []byte(`"`+u[0]+`"`), []byte(`"${`+u[1]+`}"`))
			}
			s.Steps = append(s.Steps, enginefake.Step{Emit: msg})
		}
	}
	return s, nil
}

// ruleFor answers control requests of subtype with the recorded response body.
func ruleFor(subtype string, line json.RawMessage) enginefake.Step {
	var r struct {
		Response struct {
			Subtype  string          `json:"subtype"`
			Response json.RawMessage `json:"response"`
			Error    string          `json:"error"`
		} `json:"response"`
	}
	_ = json.Unmarshal(line, &r)
	on, _ := json.Marshal(map[string]any{"type": "control_request", "request": map[string]string{"subtype": subtype}})
	st := enginefake.Step{On: on}
	if r.Response.Subtype == "error" {
		st.RespondError = r.Response.Error
		if st.RespondError == "" {
			st.RespondError = "error"
		}
	} else if len(r.Response.Response) > 0 {
		st.Respond = r.Response.Response
	} else {
		st.Respond = json.RawMessage("null")
	}
	return st
}
