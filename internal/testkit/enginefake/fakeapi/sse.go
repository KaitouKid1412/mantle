package fakeapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Usage is the Messages API usage object.
type Usage struct {
	InputTokens              int    `json:"input_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ServiceTier              string `json:"service_tier,omitempty"`
}

// Message is a complete (non-streaming) Messages API response.
type Message struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Role         string  `json:"role"`
	Model        string  `json:"model"`
	Content      []Obj   `json:"content"`
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
	Usage        Usage   `json:"usage"`
}

// Obj is a JSON object that keeps its key order (the API puts "type" first).
type Obj []KV

// KV is one member of an Obj.
type KV struct {
	K string
	V any
}

// MarshalJSON writes the members in order.
func (o Obj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(kv.K)
		v, err := json.Marshal(kv.V)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// An Event is one server-sent event: "event: <Name>" and "data: <JSON of Data>".
type Event struct {
	Name string
	Data any
}

// WriteTo writes the event in SSE framing, ending with a blank line.
func (e Event) WriteTo(w io.Writer) (int64, error) {
	data, err := json.Marshal(e.Data)
	if err != nil {
		return 0, err
	}
	n, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, data)
	return int64(n), err
}

// reply is a fully resolved answer: ids, signatures and inputs filled in.
type reply struct {
	ID         string
	Model      string
	Blocks     []Block
	StopReason string
	Usage      Usage
}

// fakeSignature is a stable, base64-looking signature for a thinking block.
func fakeSignature(thinking string) string {
	sum := sha256.Sum256([]byte("fakeapi:" + thinking))
	return "fakesig" + base64.StdEncoding.EncodeToString(sum[:])
}

// outputTokens is a rough token estimate (4 bytes per token) for the reply content.
func outputTokens(bs []Block) int {
	n := 0
	for _, b := range bs {
		n += len(b.Text) + len(b.Thinking) + len(b.Name) + len(b.Input)
	}
	return max(1, n/4)
}

// contentBlock is a finished block, as in a non-streaming response.
func contentBlock(b Block) Obj {
	switch b.Type {
	case "thinking":
		return Obj{{"type", "thinking"}, {"thinking", b.Thinking}, {"signature", b.Signature}}
	case "tool_use":
		return Obj{{"type", "tool_use"}, {"id", b.ID}, {"name", b.Name}, {"input", json.RawMessage(b.Input)}}
	default:
		return Obj{{"type", "text"}, {"text", b.Text}}
	}
}

// startBlock is the content_block of a content_block_start event.
func startBlock(b Block) Obj {
	switch b.Type {
	case "thinking":
		return Obj{{"type", "thinking"}, {"thinking", ""}, {"signature", ""}}
	case "tool_use":
		return Obj{{"type", "tool_use"}, {"id", b.ID}, {"name", b.Name}, {"input", Obj{}}}
	default:
		return Obj{{"type", "text"}, {"text", ""}}
	}
}

// message is the non-streaming response body.
func (r *reply) message() Message {
	stop := r.StopReason
	m := Message{
		ID: r.ID, Type: "message", Role: "assistant", Model: r.Model,
		Content: []Obj{}, StopReason: &stop, Usage: r.Usage,
	}
	for _, b := range r.Blocks {
		m.Content = append(m.Content, contentBlock(b))
	}
	return m
}

// events is the streaming response, in the Messages API event order. Text and thinking
// are split into deltas of at most chunk runes (at least two deltas when possible); tool
// input is split into three input_json_delta chunks after an empty first one.
func (r *reply) events(chunk int) []Event {
	if chunk <= 0 {
		chunk = defaultChunkRunes
	}
	startUsage := r.Usage
	startUsage.OutputTokens = 1
	evs := []Event{
		{"message_start", Obj{{"type", "message_start"}, {"message", Message{
			ID: r.ID, Type: "message", Role: "assistant", Model: r.Model,
			Content: []Obj{}, Usage: startUsage,
		}}}},
		{"ping", Obj{{"type", "ping"}}},
	}
	for i, b := range r.Blocks {
		evs = append(evs, Event{"content_block_start", Obj{
			{"type", "content_block_start"}, {"index", i}, {"content_block", startBlock(b)},
		}})
		delta := func(d Obj) {
			evs = append(evs, Event{"content_block_delta", Obj{
				{"type", "content_block_delta"}, {"index", i}, {"delta", d},
			}})
		}
		switch b.Type {
		case "thinking":
			for _, s := range splitRunes(b.Thinking, chunk) {
				delta(Obj{{"type", "thinking_delta"}, {"thinking", s}})
			}
			delta(Obj{{"type", "signature_delta"}, {"signature", b.Signature}})
		case "tool_use":
			delta(Obj{{"type", "input_json_delta"}, {"partial_json", ""}})
			for _, s := range splitN(string(b.Input), 3) {
				delta(Obj{{"type", "input_json_delta"}, {"partial_json", s}})
			}
		default:
			for _, s := range splitRunes(b.Text, chunk) {
				delta(Obj{{"type", "text_delta"}, {"text", s}})
			}
		}
		evs = append(evs, Event{"content_block_stop", Obj{{"type", "content_block_stop"}, {"index", i}}})
	}
	endUsage := r.Usage
	endUsage.ServiceTier = ""
	evs = append(evs,
		Event{"message_delta", Obj{
			{"type", "message_delta"},
			{"delta", Obj{{"stop_reason", r.StopReason}, {"stop_sequence", nil}}},
			{"usage", endUsage},
		}},
		Event{"message_stop", Obj{{"type", "message_stop"}}},
	)
	return evs
}

// splitRunes cuts s into pieces of at most n runes, and into at least two pieces when s
// has two or more runes, so that streaming is always exercised.
func splitRunes(s string, n int) []string {
	count := utf8.RuneCountInString(s)
	pieces := (count + n - 1) / n
	if count >= 2 {
		pieces = max(pieces, 2)
	}
	return splitN(s, pieces)
}

// splitN cuts s into k pieces of nearly equal rune length, on rune boundaries. It returns
// fewer pieces when s has fewer than k runes, and no pieces for "".
func splitN(s string, k int) []string {
	count := utf8.RuneCountInString(s)
	if count == 0 {
		return nil
	}
	k = min(max(k, 1), count)
	out := make([]string, 0, k)
	size, extra := count/k, count%k
	for i := 0; i < k; i++ {
		want := size
		if i < extra {
			want++
		}
		j := 0
		for n := 0; n < want; n++ {
			_, w := utf8.DecodeRuneInString(s[j:])
			j += w
		}
		out = append(out, s[:j])
		s = s[j:]
	}
	return out
}
