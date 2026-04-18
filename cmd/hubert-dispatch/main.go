// Command hubert-dispatch is the GHA-side binary that reads an
// orchestrator action and turns it into a Kubernetes Job. It
// runs inside the short-lived orchestrator workflow, carries a
// kubeconfig bound to a namespace-scoped service account, and
// applies a Job manifest with the appropriate resource tier
// and deadline.
//
// [Now] skeleton: flags and action dispatch only. Real Job
// templating lands with §6 Task 5 in PLAN.md, porting from
// docker/tools/hermes-delegate/k8s.go in the hermetic ref.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/hubert-is-a-bot/hubert/internal/dispatch"
)

func main() {
	var cfg dispatch.Config
	flag.StringVar(&cfg.Repo, "repo", "", "target repository as owner/name")
	flag.StringVar(&cfg.Namespace, "namespace", "hubert", "kubernetes namespace")
	flag.StringVar(&cfg.Image, "image", "", "runner container image reference")
	flag.StringVar(&cfg.ActionsFile, "actions", "-", "path to hubert-actions JSON (- for stdin)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	actions, err := readActions(cfg.ActionsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hubert-dispatch: read actions: %v\n", err)
		os.Exit(1)
	}

	if err := dispatch.Apply(ctx, cfg, actions); err != nil {
		fmt.Fprintf(os.Stderr, "hubert-dispatch: %v\n", err)
		os.Exit(1)
	}
}

func readActions(path string) ([]dispatch.Action, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var actions []dispatch.Action
	if err := json.NewDecoder(r).Decode(&actions); err != nil {
		return nil, fmt.Errorf("decode actions: %w", err)
	}
	return actions, nil
}
