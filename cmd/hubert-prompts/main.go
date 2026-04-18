// Command hubert-prompts prints one of the embedded runtime
// prompts to stdout. It exists so shell scripts (notably the
// GHA orchestrator workflow) can pipe the canonical prompt
// into an LLM CLI without having to re-parse the markdown
// source.
//
// Usage:
//
//	hubert-prompts orchestrator
//	hubert-prompts execution
//	hubert-prompts reviewer
package main

import (
	"fmt"
	"os"

	"github.com/hubert-is-a-bot/hubert/prompts"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hubert-prompts {orchestrator|execution|reviewer}")
		os.Exit(2)
	}
	var body string
	switch os.Args[1] {
	case "orchestrator":
		body = prompts.Orchestrator
	case "execution":
		body = prompts.Execution
	case "reviewer":
		body = prompts.Reviewer
	default:
		fmt.Fprintf(os.Stderr, "hubert-prompts: unknown prompt %q\n", os.Args[1])
		os.Exit(2)
	}
	if _, err := os.Stdout.WriteString(body); err != nil {
		fmt.Fprintf(os.Stderr, "hubert-prompts: write: %v\n", err)
		os.Exit(1)
	}
}
