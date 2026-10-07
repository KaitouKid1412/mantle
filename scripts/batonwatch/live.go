package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// live redraws a table of current locks every second until interrupted.
func live(p paths) error {
	for {
		s, err := readStable(p.State)
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString("\x1b[H\x1b[2J")
		fmt.Fprintf(&b, "file-baton  %s  %d sessions  %d locks\n\n", time.Now().Format("15:04:05"), len(s.Sessions), len(s.Locks))
		fmt.Fprintf(&b, "%-56s %-9s %-8s %s\n", "FILE", "OWNER", "STATUS", "QUEUE")
		files := make([]string, 0, len(s.Locks))
		for f := range s.Locks {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			l := s.Locks[f]
			var q []string
			for _, w := range l.Queue {
				q = append(q, short(w.Session))
			}
			fmt.Fprintf(&b, "%-56s %-9s %-8s %s\n", f, short(l.Owner), l.Status, strings.Join(q, ","))
		}
		_, _ = os.Stdout.WriteString(b.String())
		time.Sleep(time.Second)
	}
}
