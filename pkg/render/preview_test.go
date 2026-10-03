package render

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

// TestPreview prints a rendered markdown file for eyeballing:
//
//	MANTLE_PREVIEW=testdata/sample.md MANTLE_WIDTH=60 go test ./pkg/render -run TestPreview -v
func TestPreview(t *testing.T) {
	path := os.Getenv("MANTLE_PREVIEW")
	if path == "" {
		t.Skip("set MANTLE_PREVIEW to a markdown file")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	width := 80
	if w, err := strconv.Atoi(os.Getenv("MANTLE_WIDTH")); err == nil {
		width = w
	}
	for _, l := range Markdown(string(src), MarkdownOptions{Width: width, Palette: testPalette}) {
		if os.Getenv("MANTLE_PLAIN") != "" {
			l = Strip(l)
		}
		fmt.Println(l + "\x1b[0m|")
	}
}
