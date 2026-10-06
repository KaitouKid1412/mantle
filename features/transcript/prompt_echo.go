package transcript

import (
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Prompt echoes (PARITY TR-06; `!` commands AC-17). The engine replays a prompt
// only once it takes it up, which can be seconds after Enter (hooks, MCP
// startup). Claude Code shows the prompt at once, so the input feature sends a
// running user item, "user:<uuid>" with the prompt's uuid, in a
// TranscriptHistoryMsg as it sends a prompt that runs next (not one queued
// behind a turn). The echo stays in the live area until:
//
//   - the engine's replay of that uuid arrives: it settles the echo in place
//     (one line, the engine's version of the message);
//   - its owner sends it again, finished (a `!` command whose output is in);
//   - the turn visibly starts or ends without a replay, or the engine is
//     stopped: it is kept as it is;
//   - the prompt never reaches the engine (the send fails, the engine fails
//     to start or dies first): it is dropped, and the input feature puts the
//     prompt back in the box.

// isEcho reports whether a history item is a prompt echo.
func isEcho(it *ext.Item) bool {
	return it.ParentID == "" && it.State == ext.Running && strings.HasPrefix(it.ID, "user:") &&
		(it.Key == ext.KeyUserPrompt || it.Key == ext.KeyUserBash)
}

// updateEcho applies an echo sent again by its owner.
func (s *Store) updateEcho(old, it *ext.Item) {
	if old.State.Finished() {
		return // the replay came first
	}
	old.Key, old.Data = it.Key, it.Data
	if it.State.Finished() {
		s.finish(old, it.State)
	} else {
		s.touch(old)
	}
}

// settleEcho replaces an echo's data with the engine's replay of it.
func (s *Store) settleEcho(id string, key ext.ContentKey, data any) {
	delete(s.echoes, id)
	it := s.byID[id]
	if it == nil {
		return
	}
	it.Key, it.Data = key, data
	if it.State.Finished() {
		s.touch(it)
	} else {
		s.finish(it, ext.Done)
	}
}

// finishEchoes keeps the prompt echoes still waiting for their replay, so the
// turn's output does not queue behind them. A `!` command still running stays.
func (s *Store) finishEchoes() {
	for id := range s.echoes {
		if it := s.byID[id]; it != nil && it.Key == ext.KeyUserPrompt && !it.State.Finished() {
			s.finish(it, ext.Done)
		}
	}
}

// dropEchoes removes the prompt echoes still waiting for their replay (the
// engine died before taking them up).
func (s *Store) dropEchoes() {
	for id := range s.echoes {
		if it := s.byID[id]; it != nil && it.Key == ext.KeyUserPrompt && !it.State.Finished() {
			delete(s.echoes, id)
			s.remove(id)
		}
	}
}

// DropEcho removes the echo of a prompt whose send failed. It reports whether
// there was one still on screen to remove.
func (s *Store) DropEcho(uuid string) bool {
	id := "user:" + uuid
	if !s.echoes[id] {
		return false
	}
	delete(s.echoes, id)
	it := s.byID[id]
	if it == nil || it.State.Finished() {
		return false
	}
	s.remove(id)
	return true
}
