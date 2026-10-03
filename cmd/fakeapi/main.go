// Command fakeapi is a scripted Anthropic Messages API mock, for offline end-to-end tests
// of the real claude binary.
//
// Usage:
//
//	fakeapi [-script turns.json] [-addr 127.0.0.1:0] [-record requests.jsonl]
//	        [-seed-config DIR [-trust DIR]...] [-v]
//
// It prints the base URL (http://127.0.0.1:PORT) as the first stdout line, then serves
// until SIGINT or SIGTERM. Point claude at it with an isolated config directory:
//
//	CLAUDE_CONFIG_DIR=DIR ANTHROPIC_BASE_URL=<url> \
//	ANTHROPIC_API_KEY=sk-ant-fake-key-for-tests-0000000000 \
//	CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 DISABLE_TELEMETRY=1 DISABLE_AUTOUPDATER=1 \
//	claude ...
//
// -seed-config writes DIR/.claude.json (onboarding done, the fake key approved, each
// -trust directory trusted) before serving. See package fakeapi for the script format.
//
// Primary owner: plan 02.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("fakeapi", flag.ContinueOnError)
	scriptPath := fs.String("script", "", "JSON script of turns (default: answer everything with \"OK\")")
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	record := fs.String("record", "", "append every request as one JSON line to this file")
	seed := fs.String("seed-config", "", "write DIR/.claude.json for an isolated CLAUDE_CONFIG_DIR, then serve")
	apiKey := fs.String("api-key", fakeapi.FakeAPIKey, "API key to pre-approve with -seed-config")
	verbose := fs.Bool("v", false, "log every request to stderr")
	chunkDelay := fs.Duration("chunk-delay", 0, "sleep between streamed events")
	var trust multiFlag
	fs.Var(&trust, "trust", "directory to mark trusted with -seed-config (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	script := &fakeapi.Script{}
	if *scriptPath != "" {
		var err error
		if script, err = fakeapi.ParseFile(*scriptPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if *seed != "" {
		if err := fakeapi.SeedConfig(*seed, *apiKey, trust...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}

	logger := log.New(os.Stderr, "", log.Ltime|log.Lmicroseconds)
	opts := []fakeapi.Option{fakeapi.WithChunkDelay(*chunkDelay)}
	if *verbose {
		opts = append(opts, fakeapi.WithLogf(logger.Printf))
	} else {
		// Unknown paths are always worth seeing.
		opts = append(opts, fakeapi.WithLogf(func(format string, a ...any) {
			if strings.HasPrefix(format, "fakeapi: unknown path") {
				logger.Printf(format, a...)
			}
		}))
	}
	if *record != "" {
		f, err := os.OpenFile(*record, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer f.Close()
		var mu sync.Mutex
		enc := json.NewEncoder(f)
		opts = append(opts, fakeapi.WithRecorder(func(r fakeapi.Request) {
			mu.Lock()
			defer mu.Unlock()
			if err := enc.Encode(r); err != nil {
				logger.Printf("fakeapi: record: %v", err)
			}
		}))
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("http://%s\n", ln.Addr())
	srv := &http.Server{Handler: fakeapi.New(script, opts...), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		fmt.Fprintln(os.Stderr, err)
		return 1
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return 0
}
