package engine

import (
	"encoding/json"
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// inboundSet tracks CLI → client control requests until they are answered exactly
// once or withdrawn by control_cancel_request (B4).
type inboundSet struct {
	mu      sync.Mutex
	pending map[string]string // request id -> subtype
	seen    map[string]bool   // every request id ever handled (dedupe replays)
}

func newInboundSet() *inboundSet {
	return &inboundSet{pending: map[string]string{}, seen: map[string]bool{}}
}

// first reports whether id is new, and marks it seen. The engine may deliver one
// request twice (live, and again in an initialize reply's pending list).
func (s *inboundSet) first(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[id] {
		return false
	}
	s.seen[id] = true
	return true
}

// drainPeek returns the pending ids without removing them.
func (s *inboundSet) drainPeek() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	return ids
}

func (s *inboundSet) add(id, subtype string) {
	s.mu.Lock()
	s.pending[id] = subtype
	s.mu.Unlock()
}

// take removes id; ok is false if it was already answered or cancelled.
func (s *inboundSet) take(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[id]
	delete(s.pending, id)
	return ok
}

// drain removes and returns every pending id.
func (s *inboundSet) drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	clear(s.pending)
	return ids
}

// handleRequest turns a CLI → client control request into a UI message, or answers
// it directly when mantle has nothing to ask the user.
func (r *run) handleRequest(cr *proto.ControlRequest) {
	id := cr.RequestID
	if !r.inbound.first(id) {
		return
	}
	sub := cr.RequestSubtype()
	switch sub {
	case proto.SubCanUseTool:
		v, _ := cr.DecodeRequest()
		req, _ := v.(*proto.CanUseTool)
		if req == nil {
			req = &proto.CanUseTool{}
		}
		r.inbound.add(id, sub)
		toolUseID := req.ToolUseID
		r.coal.PushMsg(ext.PermissionMsg{
			EngineID:  r.e.id,
			RequestID: id,
			Req:       *req,
			Reply: func(res proto.PermissionResult) tea.Cmd {
				return func() tea.Msg {
					if res.ToolUseID == "" {
						res.ToolUseID = toolUseID
					}
					r.reply(id, res, nil)
					return nil
				}
			},
		})
	case proto.SubHookCallback:
		// mantle registers no hook callbacks yet: "no opinion".
		r.answer(id, proto.HookOutput{}, nil)
	case proto.SubMCPMessage:
		r.answer(id, nil, fmt.Errorf("mantle runs no SDK MCP servers"))
	case proto.SubElicitation, proto.SubRequestUserDialog:
		r.inbound.add(id, sub)
		r.coal.PushMsg(ext.ControlRequestMsg{
			EngineID:  r.e.id,
			Subtype:   sub,
			RequestID: id,
			Request:   cr.Request,
			Reply: func(resp any, err error) tea.Cmd {
				return func() tea.Msg {
					r.reply(id, resp, err)
					return nil
				}
			},
		})
	default:
		r.answer(id, nil, fmt.Errorf("unsupported control request subtype in mantle: %s", sub))
	}
}

// handleCancel withdraws a CLI request: close its dialog, never reply.
func (r *run) handleCancel(id string) {
	if r.inbound.take(id) {
		r.coal.PushMsg(ext.ControlCancelMsg{EngineID: r.e.id, RequestID: id})
	}
}

// reply answers a tracked request exactly once; later or cancelled replies are dropped.
func (r *run) reply(id string, resp any, err error) {
	if !r.inbound.take(id) {
		return
	}
	r.answer(id, resp, err)
}

func (r *run) answer(id string, resp any, err error) {
	var line []byte
	var merr error
	if err != nil {
		line, merr = proto.MarshalControlError(id, err.Error())
	} else {
		if raw, ok := resp.(json.RawMessage); ok && len(raw) == 0 {
			resp = nil
		}
		line, merr = proto.MarshalControlSuccess(id, resp)
	}
	if merr != nil {
		line, _ = proto.MarshalControlError(id, "mantle: "+merr.Error())
	}
	if serr := r.tr.Send(line); serr != nil {
		r.e.mgr.logf("engine %s: reply %s: %v", r.e.id, id, serr)
	}
}
