package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// wire is the union of every typed record's fields, so a line is decoded in one pass.
type wire struct {
	Type string `json:"type"`

	UUID                      string           `json:"uuid"`
	ParentUUID                *string          `json:"parentUuid"`
	LogicalParentUUID         string           `json:"logicalParentUuid"`
	IsSidechain               bool             `json:"isSidechain"`
	Timestamp                 string           `json:"timestamp"`
	SessionID                 string           `json:"sessionId"`
	AgentID                   string           `json:"agentId"`
	Cwd                       string           `json:"cwd"`
	GitBranch                 string           `json:"gitBranch"`
	Version                   string           `json:"version"`
	Entrypoint                string           `json:"entrypoint"`
	UserType                  string           `json:"userType"`
	SessionKind               string           `json:"sessionKind"`
	Slug                      string           `json:"slug"`
	IsMeta                    bool             `json:"isMeta"`
	PromptID                  string           `json:"promptId"`
	PermissionMode            string           `json:"permissionMode"`
	Message                   *Message         `json:"message"`
	ToolUseResult             json.RawMessage  `json:"toolUseResult"`
	SourceToolAssistantUUID   string           `json:"sourceToolAssistantUUID"`
	SourceToolUseID           string           `json:"sourceToolUseID"`
	IsCompactSummary          bool             `json:"isCompactSummary"`
	IsVisibleInTranscriptOnly bool             `json:"isVisibleInTranscriptOnly"`
	RequestID                 string           `json:"requestId"`
	IsAPIErrorMessage         bool             `json:"isApiErrorMessage"`
	Attachment                json.RawMessage  `json:"attachment"`
	Subtype                   string           `json:"subtype"`
	Content                   json.RawMessage  `json:"content"`
	Level                     string           `json:"level"`
	DurationMs                float64          `json:"durationMs"`
	MessageCount              int              `json:"messageCount"`
	CompactMetadata           *CompactMetadata `json:"compactMetadata"`

	LastPrompt      string          `json:"lastPrompt"`
	LeafUUID        string          `json:"leafUuid"`
	AITitle         string          `json:"aiTitle"`
	CustomTitle     string          `json:"customTitle"`
	AgentName       string          `json:"agentName"`
	Summary         string          `json:"summary"`
	Tag             string          `json:"tag"`
	Mode            string          `json:"mode"`
	RelocatedCwd    string          `json:"relocatedCwd"`
	PRNumber        int             `json:"prNumber"`
	PRURL           string          `json:"prUrl"`
	PRRepository    string          `json:"prRepository"`
	WorktreeSession json.RawMessage `json:"worktreeSession"`
	Operation       string          `json:"operation"`

	MessageID         string          `json:"messageId"`
	SnapshotMessageID string          `json:"snapshotMessageId"`
	TrackingPath      string          `json:"trackingPath"`
	Backup            json.RawMessage `json:"backup"`
	IsSnapshotUpdate  bool            `json:"isSnapshotUpdate"`
	Snapshot          *struct {
		MessageID          string `json:"messageId"`
		Timestamp          string `json:"timestamp"`
		TrackedFileBackups map[string]struct {
			BackupFileName string `json:"backupFileName"`
			Version        int    `json:"version"`
			BackupTime     string `json:"backupTime"`
		} `json:"trackedFileBackups"`
	} `json:"snapshot"`

	TotalCostUSD                   float64               `json:"totalCostUSD"`
	TotalAPIDuration               float64               `json:"totalAPIDuration"`
	TotalAPIDurationWithoutRetries float64               `json:"totalAPIDurationWithoutRetries"`
	TotalToolDuration              float64               `json:"totalToolDuration"`
	TotalDuration                  float64               `json:"totalDuration"`
	TotalLinesAdded                int64                 `json:"totalLinesAdded"`
	TotalLinesRemoved              int64                 `json:"totalLinesRemoved"`
	StartTime                      float64               `json:"startTime"`
	ModelUsage                     map[string]ModelUsage `json:"modelUsage"`
	HasUnknownModelCost            bool                  `json:"hasUnknownModelCost"`
}

// ErrMalformed wraps a line that is not a JSON object.
var ErrMalformed = errors.New("malformed transcript line")

// Decode decodes one transcript line. A field of an unexpected JSON type is dropped
// rather than failing the record, so newer engines' records still decode. It returns
// ErrMalformed (wrapped) if the line is not a JSON object.
func Decode(line []byte, pos Pos) (Record, error) {
	line = trimLine(line)
	if len(line) == 0 || line[0] != '{' {
		return nil, ErrMalformed
	}
	var w wire
	if err := json.Unmarshal(line, &w); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return nil, errors.Join(ErrMalformed, err)
		}
	}
	r := rec{kind: w.Type, raw: json.RawMessage(line), pos: pos}
	switch w.Type {
	case KindUser, KindAssistant, KindAttachment, KindSystem, KindProgress:
		return w.entry(r), nil
	case KindLastPrompt:
		return &LastPrompt{rec: r, SessionID: w.SessionID, LastPrompt: w.LastPrompt, LeafUUID: w.LeafUUID}, nil
	case KindAITitle:
		return &AITitle{rec: r, SessionID: w.SessionID, Title: w.AITitle}, nil
	case KindCustomTitle:
		return &CustomTitle{rec: r, SessionID: w.SessionID, Title: w.CustomTitle}, nil
	case KindAgentName:
		return &AgentName{rec: r, SessionID: w.SessionID, Name: w.AgentName}, nil
	case KindSummary:
		return &Summary{rec: r, Summary: w.Summary, LeafUUID: w.LeafUUID}, nil
	case KindTag:
		return &Tag{rec: r, SessionID: w.SessionID, Tag: w.Tag}, nil
	case KindMode:
		return &Mode{rec: r, SessionID: w.SessionID, Mode: w.Mode}, nil
	case KindPermissionMode:
		return &PermissionMode{rec: r, SessionID: w.SessionID, PermissionMode: w.PermissionMode}, nil
	case KindRelocated:
		return &Relocated{rec: r, SessionID: w.SessionID, RelocatedCwd: w.RelocatedCwd}, nil
	case KindPRLink:
		return &PRLink{rec: r, SessionID: w.SessionID, PRNumber: w.PRNumber, PRURL: w.PRURL,
			PRRepository: w.PRRepository, Timestamp: parseTime(w.Timestamp)}, nil
	case KindWorktreeState:
		return &WorktreeState{rec: r, SessionID: w.SessionID, WorktreeSession: w.WorktreeSession}, nil
	case KindQueueOperation:
		return &QueueOperation{rec: r, SessionID: w.SessionID, Operation: w.Operation,
			Content: rawString(w.Content), Timestamp: parseTime(w.Timestamp)}, nil
	case KindCostState:
		return &CostState{
			rec: r, SessionID: w.SessionID, TotalCostUSD: w.TotalCostUSD,
			APIDuration:               ms(w.TotalAPIDuration),
			APIDurationWithoutRetries: ms(w.TotalAPIDurationWithoutRetries),
			ToolDuration:              ms(w.TotalToolDuration),
			Duration:                  ms(w.TotalDuration),
			LinesAdded:                w.TotalLinesAdded, LinesRemoved: w.TotalLinesRemoved,
			StartTime:           msTime(w.StartTime),
			ModelUsage:          w.ModelUsage,
			HasUnknownModelCost: w.HasUnknownModelCost,
		}, nil
	case KindFileHistorySnapshot:
		s := &FileHistorySnapshot{rec: r, MessageID: w.MessageID, IsSnapshotUpdate: w.IsSnapshotUpdate}
		if w.Snapshot != nil {
			if s.MessageID == "" {
				s.MessageID = w.Snapshot.MessageID
			}
			s.Timestamp = parseTime(w.Snapshot.Timestamp)
			s.Backups = make(map[string]FileBackup, len(w.Snapshot.TrackedFileBackups))
			for path, b := range w.Snapshot.TrackedFileBackups {
				s.Backups[path] = FileBackup{BackupFileName: b.BackupFileName, Version: b.Version,
					BackupTime: parseTime(b.BackupTime)}
			}
		}
		return s, nil
	case KindFileHistoryDelta:
		return &FileHistoryDelta{rec: r, MessageID: w.MessageID, SnapshotMessageID: w.SnapshotMessageID,
			TrackingPath: w.TrackingPath, Backup: w.Backup, Timestamp: parseTime(w.Timestamp)}, nil
	}
	return &Unknown{rec: r}, nil
}

func (w *wire) entry(r rec) *Entry {
	e := &Entry{
		rec: r, UUID: w.UUID, LogicalParentUUID: w.LogicalParentUUID, IsSidechain: w.IsSidechain,
		Timestamp: parseTime(w.Timestamp), SessionID: w.SessionID, AgentID: w.AgentID,
		Cwd: w.Cwd, GitBranch: w.GitBranch, Version: w.Version, Entrypoint: w.Entrypoint,
		UserType: w.UserType, SessionKind: w.SessionKind, Slug: w.Slug, IsMeta: w.IsMeta,
		PromptID: w.PromptID, PermissionMode: w.PermissionMode, Message: w.Message,
		ToolUseResult: w.ToolUseResult, SourceToolAssistantUUID: w.SourceToolAssistantUUID,
		SourceToolUseID: w.SourceToolUseID, IsCompactSummary: w.IsCompactSummary,
		IsVisibleInTranscriptOnly: w.IsVisibleInTranscriptOnly, RequestID: w.RequestID,
		IsAPIErrorMessage: w.IsAPIErrorMessage, Subtype: w.Subtype, Level: w.Level,
		Duration: ms(w.DurationMs), MessageCount: w.MessageCount,
		CompactMetadata: w.CompactMetadata,
	}
	if w.ParentUUID != nil {
		e.ParentUUID = *w.ParentUUID
	}
	if r.kind == KindSystem {
		e.Content = rawString(w.Content)
	}
	if len(w.Attachment) > 0 && w.Attachment[0] == '{' {
		var a struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(w.Attachment, &a)
		e.Attachment = &Attachment{Type: a.Type, Raw: w.Attachment}
	}
	return e
}

func trimLine(b []byte) []byte {
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	return bytes.TrimSpace(b)
}

func rawString(b json.RawMessage) string {
	if len(b) == 0 || b[0] != '"' {
		return ""
	}
	var s string
	_ = json.Unmarshal(b, &s)
	return s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

func msTime(v float64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(v))
}

// Reader decodes a transcript line by line. Lines can be many megabytes; there is no
// line-length limit. Malformed lines are skipped and counted. A final line without a
// newline that does not parse is treated as a write in progress: it is not consumed,
// and Offset stays before it so a later read can resume there.
type Reader struct {
	br        *bufio.Reader
	off       int64 // offset just past the last consumed line
	line      int
	malformed int
	partial   bool
}

// NewReader reads records from r. Line numbers and offsets start at 1 and 0.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 256<<10)}
}

// NewReaderAt reads records from r, which is positioned at byte offset off (for example a
// file seeked to a previous Offset). Line numbers then count from that point.
func NewReaderAt(r io.Reader, off int64) *Reader {
	rd := NewReader(r)
	rd.off = off
	return rd
}

// Next returns the next record, or io.EOF.
func (r *Reader) Next() (Record, error) {
	for {
		line, err := r.br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}
		complete := err == nil
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		start := r.off
		if len(trimLine(line)) == 0 {
			if complete {
				r.off += int64(len(line))
				r.line++
			}
			continue
		}
		rec, derr := Decode(line, Pos{Line: r.line + 1, Offset: start})
		if derr != nil {
			if !complete {
				r.partial = true
				return nil, io.EOF
			}
			r.off += int64(len(line))
			r.line++
			r.malformed++
			continue
		}
		r.off += int64(len(line))
		r.line++
		return rec, nil
	}
}

// Offset is the byte offset just past the last consumed line.
func (r *Reader) Offset() int64 { return r.off }

// Malformed is the number of lines skipped because they did not decode.
func (r *Reader) Malformed() int { return r.malformed }

// Partial reports whether reading stopped at an incomplete final line.
func (r *Reader) Partial() bool { return r.partial }

// ReadAll reads every record from r.
func ReadAll(r io.Reader) ([]Record, *Reader, error) {
	rd := NewReader(r)
	var out []Record
	for {
		rec, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out, rd, nil
		}
		if err != nil {
			return out, rd, err
		}
		out = append(out, rec)
	}
}

// ReadFrom reads the records of path that start at or after byte offset off, and returns
// the offset to pass next time. It is how a transcript is tailed while the engine writes.
func ReadFrom(path string, off int64) ([]Record, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, off, err
	}
	defer f.Close()
	if off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return nil, off, err
		}
	}
	rd := NewReaderAt(f, off)
	var out []Record
	for {
		rec, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out, rd.Offset(), nil
		}
		if err != nil {
			return out, rd.Offset(), err
		}
		out = append(out, rec)
	}
}
