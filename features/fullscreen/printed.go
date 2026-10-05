package fullscreen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// printedBlock is something a feature printed (Ctx.Print) while the fullscreen layout
// was active: the banner, a panel's closing line, mod output. Inline it would be in
// scrollback; here it joins the document after the transcript item that was last when
// it arrived, so it scrolls, selects and searches like transcript lines.
type printedBlock struct {
	id    string
	after string   // ID of the last top-level transcript item when printed ("" = top)
	text  []string // the blocks as printed, unwrapped
}

// printed is the fullscreen stand-in for scrollback prints.
type printed struct {
	blocks   []printedBlock
	seq      int
	hadItems bool // the transcript had items since the last print reset

	// The merged document of the last layout, reused while nothing changed.
	base           *Block
	baseLen, width int
	seqAt          int
	out            []Block
	coll           []bool
	items          []*ext.Item
}

// add records blocks from an ext.PrintedMsg. A print onto an empty transcript that
// had items before starts over: the conversation was cleared or replaced, and what was
// printed with it went with it (as scrollback does with a cleared screen).
func (p *printed) add(tr ext.Transcript, blocks []string) {
	var items []*ext.Item
	if tr != nil {
		items = tr.Items()
	}
	if len(items) == 0 && p.hadItems {
		p.reset()
	}
	after := ""
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].ParentID == "" {
			after = items[i].ID
			break
		}
	}
	p.seq++
	p.blocks = append(p.blocks, printedBlock{id: fmt.Sprintf("printed-%d", p.seq), after: after, text: blocks})
}

func (p *printed) reset() {
	p.blocks, p.hadItems = nil, false
	p.seq++ // invalidates the merged document
}

// merge returns the document with the printed blocks inserted after their anchors
// (blocks, collapsible and items stay parallel; printed blocks have a nil item).
func (p *printed) merge(tr ext.Transcript, blocks []Block, coll []bool, items []*ext.Item, w int) ([]Block, []bool, []*ext.Item) {
	if len(items) > 0 {
		p.hadItems = true
	}
	if len(p.blocks) == 0 {
		return blocks, coll, items
	}
	var base *Block
	if len(blocks) > 0 {
		base = &blocks[0]
	}
	if p.out != nil && base == p.base && len(blocks) == p.baseLen && w == p.width && p.seq == p.seqAt {
		return p.out, p.coll, p.items
	}
	pos := make(map[string]int, len(items))
	for i, it := range items {
		if it != nil {
			pos[it.ID] = i
		}
	}
	at := make(map[int][]Block) // insert after this block index (-1 = before all)
	var order map[string]int    // store order, built only for anchors that aren't shown
	for _, pb := range p.blocks {
		idx := -1
		if pb.after != "" {
			i, ok := pos[pb.after]
			if !ok {
				if order == nil {
					order = storeOrder(tr)
				}
				i = lastShownBefore(order, items, pb.after)
			}
			idx = i
		}
		at[idx] = append(at[idx], Block{ID: pb.id, Lines: wrapPrinted(pb.text, w)})
	}
	out := make([]Block, 0, len(blocks)+len(p.blocks))
	outColl := make([]bool, 0, cap(out))
	outItems := make([]*ext.Item, 0, cap(out))
	push := func(bs []Block) {
		for _, b := range bs {
			out, outColl, outItems = append(out, b), append(outColl, false), append(outItems, nil)
		}
	}
	push(at[-1])
	for i := range blocks {
		out, outColl, outItems = append(out, blocks[i]), append(outColl, coll[i]), append(outItems, items[i])
		push(at[i])
	}
	p.base, p.baseLen, p.width, p.seqAt = base, len(blocks), w, p.seq
	p.out, p.coll, p.items = out, outColl, outItems
	return out, outColl, outItems
}

// storeOrder maps every transcript item ID to its position in the store.
func storeOrder(tr ext.Transcript) map[string]int {
	order := map[string]int{}
	if tr == nil {
		return order
	}
	for i, it := range tr.Items() {
		order[it.ID] = i
	}
	return order
}

// lastShownBefore is the index of the last shown block whose item comes no later than
// anchor in the store (-1 when none does, or the anchor is gone).
func lastShownBefore(order map[string]int, items []*ext.Item, anchor string) int {
	limit, ok := order[anchor]
	if !ok {
		return len(items) - 1 // the anchor is gone: keep the block at the end
	}
	best := -1
	for i, it := range items {
		if it == nil {
			continue
		}
		if j, ok := order[it.ID]; ok && j <= limit {
			best = i
		}
	}
	return best
}

// wrapPrinted lays printed blocks out at width w, as the host wraps them inline.
func wrapPrinted(text []string, w int) []string {
	var lines []string
	for _, b := range text {
		for _, l := range strings.Split(b, "\n") {
			wrapped := ansi.Wrap(l, w, "")
			for _, wl := range strings.Split(wrapped, "\n") {
				if ansi.StringWidth(wl) > w {
					wl = ansi.Truncate(wl, w, "")
				}
				lines = append(lines, wl)
			}
		}
	}
	return lines
}
