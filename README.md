# Hubert

A small two-plane tool for AI-driven development on GitHub:
short-lived GitHub Actions workflows (control plane) dispatch
Kubernetes Jobs (execution plane) that run LLM CLIs against
issue and PR state, coordinating entirely through GitHub.

**Status:** [Now]-band skeleton landed. The three binaries
build, all unit tests pass, and the K8s Job templating +
snapshot assembly + orchestrator-output parser are in place.
The system has not yet been exercised end-to-end against a
real GitHub repo and Kubernetes cluster.

## Where to read

- **[PLAN.md](PLAN.md)** — the single canonical document.
  Architecture, security model, image contract, v1
  implementation plan, and reference-deployment notes.
- **[prompts/](prompts/)** — load-bearing runtime prompts
  (embedded into the binaries via `//go:embed`):
  [orchestrator](prompts/orchestrator.md) (decides what to do),
  [execution](prompts/execution.md) (does it),
  [reviewer](prompts/reviewer.md) (checks it).
- **[.hubert/README.md.example](.hubert/README.md.example)** —
  per-repo config example, copied into watched repos.
- **[templates/workflows/](templates/workflows/)** — GHA
  workflow templates to drop into any watched repo.
- **[archive/](archive/)** — superseded design docs kept for
  reference; all content has been folded into PLAN.md.

## Building

```
make build           # produces bin/hubert-{runner,dispatch,snap}
make test            # go vet + go test ./...
docker build -t hubert-runner:dev .
```

Requires Go 1.25+.

## Layout

| Path | Purpose |
|------|---------|
| `cmd/hubert-runner/` | In-Job runner. Acquires the issue lock, invokes the LLM CLI, heartbeats, releases. |
| `cmd/hubert-dispatch/` | GHA-side Job submitter. Reads an orchestrator action list and applies K8s Jobs. |
| `cmd/hubert-snap/` | Per-repo snapshot builder. Shells out to `gh` and emits the JSON the orchestrator consumes. |
| `internal/githubapi/` | `gh` CLI wrapper: issues, PRs, comments, labels, collaborators. |
| `internal/runner/` | Lock protocol, heartbeat, CLI shell-out, recovery-marker parser, worktree prep. |
| `internal/dispatch/` | `batch/v1` Job templating + `kubectl apply` shell-out. |
| `internal/orchestrator/` | Parser for the ```hubert-actions fenced block. |
| `internal/snapshot/` | Assembly of the per-tick GitHub state snapshot. |
| `internal/budget/` | Cost tally types (scaffolding; real accounting is [Soon]). |
| `prompts/` | Canonical prompts + `//go:embed` wiring. |
| `templates/workflows/` | GHA workflow templates watched repos drop in. |
