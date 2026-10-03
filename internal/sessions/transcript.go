package sessions

import (
	"io"
	"os"
)

// Metadata is the session-level state carried by metadata records. For each field the
// last record wins, as in the engine.
type Metadata struct {
	SessionID      string
	LastPrompt     string
	LeafUUID       string // from the last last-prompt record
	AITitle        string
	CustomTitle    string
	AgentName      string
	Summary        string // older engines' summary record
	Tag            string
	Mode           string
	PermissionMode string
	RelocatedCwd   string
	PR             *PRLink
	Cost           *CostState
}

// Apply folds one record into m (last wins). Records without metadata are ignored.
func (m *Metadata) Apply(r Record) {
	switch r := r.(type) {
	case *LastPrompt:
		if r.LastPrompt != "" {
			m.LastPrompt = r.LastPrompt
		}
		if r.LeafUUID != "" {
			m.LeafUUID = r.LeafUUID
		}
		m.setID(r.SessionID)
	case *AITitle:
		m.AITitle = r.Title
		m.setID(r.SessionID)
	case *CustomTitle:
		m.CustomTitle = r.Title
		m.setID(r.SessionID)
	case *AgentName:
		m.AgentName = r.Name
	case *Summary:
		m.Summary = r.Summary
	case *Tag:
		m.Tag = r.Tag
	case *Mode:
		m.Mode = r.Mode
	case *PermissionMode:
		m.PermissionMode = r.PermissionMode
	case *Relocated:
		m.RelocatedCwd = r.RelocatedCwd
	case *PRLink:
		m.PR = r
	case *CostState:
		m.Cost = r
	}
}

func (m *Metadata) setID(id string) {
	if id != "" {
		m.SessionID = id
	}
}

// Title is the name to show for the session: the user's custom title, then the
// generated title, then the agent name, then a legacy summary. It is "" if none exist
// (callers fall back to the first prompt).
func (m *Metadata) Title() string {
	for _, s := range []string{m.CustomTitle, m.AITitle, m.AgentName, m.Summary} {
		if s != "" {
			return s
		}
	}
	return ""
}

// Transcript is a fully parsed session file.
type Transcript struct {
	Path      string
	Records   []Record // every record, in file order
	Entries   []*Entry // message records, in file order
	Tree      *Tree
	Meta      Metadata
	Malformed int   // lines skipped because they did not decode
	Partial   bool  // the file ended in an incomplete line (still being written)
	Size      int64 // bytes consumed (the offset to tail from)
}

// Load parses the transcript at path.
func Load(path string) (*Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t, err := Parse(f)
	if t != nil {
		t.Path = path
	}
	return t, err
}

// Parse parses a transcript from r.
func Parse(r io.Reader) (*Transcript, error) {
	recs, rd, err := ReadAll(r)
	t := &Transcript{Records: recs, Malformed: rd.Malformed(), Partial: rd.Partial(), Size: rd.Offset()}
	for _, rec := range recs {
		if e, ok := rec.(*Entry); ok {
			t.Entries = append(t.Entries, e)
			if t.Meta.SessionID == "" && e.SessionID != "" && !e.IsSidechain {
				t.Meta.SessionID = e.SessionID
			}
			continue
		}
		t.Meta.Apply(rec)
	}
	t.Tree = NewTree(t.Entries)
	return t, err
}

// Append adds records read later (see ReadFrom) and rebuilds the tree.
func (t *Transcript) Append(recs []Record, newSize int64) {
	for _, rec := range recs {
		t.Records = append(t.Records, rec)
		if e, ok := rec.(*Entry); ok {
			t.Entries = append(t.Entries, e)
			continue
		}
		t.Meta.Apply(rec)
	}
	t.Size = newSize
	t.Tree = NewTree(t.Entries)
}

// ActiveLeaf is the leaf a resume continues from.
func (t *Transcript) ActiveLeaf() *Node { return t.Tree.ActiveLeaf(t.Meta.LeafUUID) }

// Main returns the active branch from root to leaf with sidechains dropped: the
// conversation as the user saw it.
func (t *Transcript) Main(opts BranchOptions) []*Entry {
	leaf := t.ActiveLeaf()
	if leaf == nil {
		return nil
	}
	return t.Tree.Branch(leaf, opts)
}

// FirstPrompt is a preview of the first prompt on the main branch (falling back to the
// first slash command's name).
func (t *Transcript) FirstPrompt() string {
	cmd := ""
	for _, e := range t.Main(BranchOptions{AcrossCompaction: true}) {
		p, c, ok := PromptPreview(e)
		if ok {
			return p
		}
		if cmd == "" {
			cmd = c
		}
	}
	return cmd
}

// Title is the session's display title, falling back to the first prompt.
func (t *Transcript) Title() string {
	if s := t.Meta.Title(); s != "" {
		return s
	}
	return t.FirstPrompt()
}
