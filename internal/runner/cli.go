package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CLIInvocation is the input to a CLI shell-out. Cwd is the
// working directory the CLI runs in (typically the cloned
// target repo); Agent and Model select the backend and its
// model; Prompt is the text fed on stdin.
type CLIInvocation struct {
	Cwd    string
	Agent  string
	Model  string
	Prompt string
}

// CLIInvoker launches an LLM CLI with a prompt on stdin and
// streams combined stdout/stderr. The returned string is the
// full combined output, used for recovery-marker parsing; the
// stderr is also teed to the parent process's stderr so the
// pod log reflects progress live.
type CLIInvoker func(ctx context.Context, inv CLIInvocation) (string, error)

// DefaultCLIInvoker is the real implementation. It selects the
// backend by agent name and shells out with the matching flag
// surface per PLAN.md §6 Task 2.
func DefaultCLIInvoker(ctx context.Context, inv CLIInvocation) (string, error) {
	name, args, err := cliArgs(inv.Agent, inv.Model)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = inv.Cwd
	cmd.Stdin = strings.NewReader(inv.Prompt)
	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(&captured, os.Stdout)
	cmd.Stderr = io.MultiWriter(&captured, os.Stderr)
	if err := cmd.Run(); err != nil {
		return captured.String(), fmt.Errorf("%s: %w", name, err)
	}
	return captured.String(), nil
}

// cliArgs maps an (agent, model) pair to the CLI command and
// flag set. The three supported backends are claude, opencode,
// and gemini; an empty model is allowed (caller's default).
func cliArgs(agent, model string) (string, []string, error) {
	switch agent {
	case "", "claude":
		args := []string{"--print"}
		if model != "" {
			args = append(args, "--model", model)
		}
		return "claude", args, nil
	case "opencode":
		args := []string{"run", "--dangerously-skip-permissions"}
		if model != "" {
			args = append(args, "-m", model)
		}
		return "opencode", args, nil
	case "gemini":
		args := []string{"-p", "-", "--yolo"}
		if model != "" {
			args = append(args, "-m", model)
		}
		return "gemini", args, nil
	default:
		return "", nil, fmt.Errorf("unknown agent %q", agent)
	}
}

// ErrCLIMissing indicates the configured backend binary is not
// on $PATH. The runner surfaces this as a `need-backend:
// alternate` hint.
var ErrCLIMissing = errors.New("cli binary not found on PATH")
