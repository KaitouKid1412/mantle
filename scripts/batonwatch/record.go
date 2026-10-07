package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// record polls file-baton's state and log until interrupted, appending events.
func record(p paths, interval time.Duration) error {
	if err := os.MkdirAll(filepath.Dir(p.Events), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(p.Events, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	// Start from the log's end so a restarted recorder does not replay old
	// decisions; locks already held show up as acquires at start.
	prev, err := readStable(p.State)
	if err != nil {
		return err
	}
	logOff := size(p.Log)
	now := time.Now()
	_ = enc.Encode(event{T: now, Kind: "start", Msg: fmt.Sprintf("%d locks, %d sessions", len(prev.Locks), len(prev.Sessions))})
	for _, e := range diff(&state{}, prev, now) {
		_ = enc.Encode(e)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fmt.Fprintf(os.Stderr, "batonwatch: recording to %s (ctrl+c to stop)\n", p.Events)

	for {
		select {
		case <-stop:
			_ = enc.Encode(event{T: time.Now(), Kind: "stop"})
			return nil
		case <-tick.C:
		}
		var lines []string
		lines, logOff = tail(p.Log, logOff)
		for _, l := range lines {
			if ll, ok := parseLog(l); ok {
				_ = enc.Encode(event{T: ll.T, Kind: evLog, SID: ll.SID, Reason: ll.Event, Msg: ll.Msg})
			}
		}
		cur, err := readStable(p.State)
		if err != nil {
			continue // mid-write; try again next tick
		}
		for _, e := range diff(prev, cur, time.Now()) {
			_ = enc.Encode(e)
			fmt.Fprintln(os.Stderr, describe(e))
		}
		prev = cur
	}
}

// readStable reads the state, retrying briefly when it is caught mid-write.
func readStable(path string) (*state, error) {
	var err error
	for range 5 {
		var s *state
		if s, err = readState(path); err == nil {
			return s, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, err
}

func size(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// tail returns the complete lines appended after off, and the new offset.
func tail(path string, off int64) ([]string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, off
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() < off {
		off = 0 // truncated or replaced
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off
	}
	var lines []string
	r := bufio.NewReader(f)
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			break // a partial last line is read again next time
		}
		off += int64(len(s))
		lines = append(lines, strings.TrimRight(s, "\n"))
	}
	return lines, off
}

func describe(e event) string {
	t := e.T.Format("15:04:05.000")
	switch e.Kind {
	case evHandoff:
		return fmt.Sprintf("%s %-9s %s  %s -> %s (%s)", t, e.Kind, e.File, e.Other, e.SID, e.Reason)
	case evDelivered:
		return fmt.Sprintf("%s %-9s %s  to %s from %s", t, e.Kind, e.File, e.SID, e.Other)
	default:
		return fmt.Sprintf("%s %-9s %s  %s %s", t, e.Kind, e.File, e.SID, e.Status)
	}
}
