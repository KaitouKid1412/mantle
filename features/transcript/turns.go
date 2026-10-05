package transcript

import (
	"sort"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// The headless engine does not write turn-duration records into the session
// file (interactive Claude Code does), so a resumed session would lose its
// "✻ … for <dur> · done <time>" lines. The transcript keeps its own small
// ledger, in mantle's state store: for every finished model turn, the uuid of
// its final answer, the duration and the end time. When history is loaded, the
// turn line goes back after that answer.

const (
	ledgerKey  = "turn-durations"
	ledgerSize = 2000 // newest entries kept
)

type turnEntry struct {
	MS  int64     `json:"ms"`
	End time.Time `json:"end"`
}

func (f *Feature) loadLedger(c ext.Ctx) map[string]turnEntry {
	if f.ledger == nil {
		f.ledger = map[string]turnEntry{}
		if kv := c.Store(FeatureID); kv != nil {
			_, _ = kv.Get(ledgerKey, &f.ledger)
		}
	}
	return f.ledger
}

// recordTurn remembers a finished model turn's duration under its final
// answer's message uuid.
func (f *Feature) recordTurn(c ext.Ctx, r *proto.Result) {
	if !modelTurn(r) || r.NumTurns == 0 {
		return
	}
	uuid := f.store.LastAnswerUUID()
	if uuid == "" {
		return
	}
	l := f.loadLedger(c)
	l[uuid] = turnEntry{MS: r.DurationMS, End: f.store.now()}
	if len(l) > ledgerSize {
		type kv struct {
			k string
			t time.Time
		}
		all := make([]kv, 0, len(l))
		for k, e := range l {
			all = append(all, kv{k, e.End})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		for _, e := range all[:len(l)-ledgerSize] {
			delete(l, e.k)
		}
	}
	if kv := c.Store(FeatureID); kv != nil {
		_ = kv.Set(ledgerKey, l)
	}
}

// withTurnLines adds the recorded turn lines to history items: after the last
// block of each answer the ledger knows, unless the session file already gave
// that turn a line (interactive sessions record their own).
func (f *Feature) withTurnLines(c ext.Ctx, items []*ext.Item) []*ext.Item {
	l := f.loadLedger(c)
	if len(l) == 0 {
		return items
	}
	out := make([]*ext.Item, 0, len(items))
	for i, it := range items {
		out = append(out, it)
		uuid, ok := answerUUID(it)
		if !ok {
			continue
		}
		e, ok := l[uuid]
		if !ok {
			continue
		}
		if i+1 < len(items) {
			next := items[i+1]
			if nu, ok := answerUUID(next); ok && nu == uuid {
				continue // more blocks of the same answer follow
			}
			if next.Key == KeyResult {
				continue // the session file has its own line
			}
		}
		out = append(out, &ext.Item{
			ID:  "result:ledger:" + uuid,
			Key: KeyResult,
			Data: &proto.Result{
				Envelope:   proto.Envelope{Type: proto.TypeResult, Subtype: proto.ResultSuccess, UUID: "ledger-" + uuid},
				DurationMS: e.MS,
				NumTurns:   1,
			},
			State: ext.Done,
			Start: e.End,
			End:   e.End,
		})
	}
	return out
}

// answerUUID returns the message uuid of a top-level answer item from history
// (plan 06 names them "txt:<uuid>:<block>").
func answerUUID(it *ext.Item) (string, bool) {
	if it == nil || it.Key != ext.KeyAssistantText || it.ParentID != "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(it.ID, "txt:")
	if !ok {
		return "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}
