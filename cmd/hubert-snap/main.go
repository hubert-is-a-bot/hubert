// Command hubert-snap builds a per-tick GitHub state snapshot
// for one repository and writes it to stdout as JSON. The
// orchestrator prompt consumes this snapshot to decide what
// work to dispatch next.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
	"github.com/hubert-is-a-bot/hubert/internal/snapshot"
)

func main() {
	var (
		repo   = flag.String("repo", "", "target repository as owner/name")
		maxAge = flag.Duration("max-age", 30*time.Minute, "maximum age for a cached snapshot")
	)
	flag.Parse()
	_ = maxAge

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "hubert-snap: -repo is required")
		os.Exit(2)
	}

	client := githubapi.NewClient(*repo)
	snap, err := snapshot.Capture(ctx, client, *repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hubert-snap: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		fmt.Fprintf(os.Stderr, "hubert-snap: encode: %v\n", err)
		os.Exit(1)
	}
}
