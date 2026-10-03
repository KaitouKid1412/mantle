// Package fixture records engine traffic (both directions, timestamped), sanitises
// it for committing, and turns recordings back into fakeclaude scripts.
//
// File format (testdata/fixtures/02/<name>.jsonl), one entry per line:
//
//	{"t":123,"dir":"in","msg":{...}}   client → engine (stdin); t = ms since start
//	{"t":130,"dir":"out","msg":{...}}  engine → client (stdout)
//
// Primary owner: plan 02.
package fixture

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Directions.
const (
	In  = "in"  // to the engine (stdin)
	Out = "out" // from the engine (stdout)
)

// Entry is one recorded line.
type Entry struct {
	T   int64           `json:"t"`
	Dir string          `json:"dir"`
	Msg json.RawMessage `json:"msg"`
}

// Recorder writes entries as they happen. It is safe for concurrent use.
type Recorder struct {
	mu    sync.Mutex
	w     io.Writer
	start time.Time
	san   *Sanitizer // nil = raw
	err   error
}

// NewRecorder writes to w, sanitising with san (nil records raw lines; never commit
// those).
func NewRecorder(w io.Writer, san *Sanitizer) *Recorder {
	return &Recorder{w: w, start: time.Now(), san: san}
}

// Record adds one line. Lines that are not JSON objects are skipped.
func (r *Recorder) Record(dir string, line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' || !json.Valid(line) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.san != nil {
		line = r.san.Line(line)
	}
	b, err := json.Marshal(Entry{T: time.Since(r.start).Milliseconds(), Dir: dir, Msg: line})
	if err == nil {
		b = append(b, '\n')
		_, err = r.w.Write(b)
	}
	if err != nil && r.err == nil {
		r.err = err
	}
}

// Err returns the first write error.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Load reads a fixture file.
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read parses fixture entries.
func Read(r io.Reader) ([]Entry, error) {
	var out []Entry
	br := bufio.NewReaderSize(r, 1<<20)
	for n := 1; ; n++ {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			var e Entry
			if uerr := json.Unmarshal(t, &e); uerr != nil {
				return nil, fmt.Errorf("fixture line %d: %w", n, uerr)
			}
			out = append(out, e)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// Outputs returns the engine → client lines, in order.
func Outputs(entries []Entry) [][]byte {
	var out [][]byte
	for _, e := range entries {
		if e.Dir == Out {
			out = append(out, e.Msg)
		}
	}
	return out
}
