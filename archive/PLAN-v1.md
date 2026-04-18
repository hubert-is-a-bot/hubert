# Hubert v1 — implementation plan

## Context

Hubert is the project designed in [`ARCHITECTURE.md`](ARCHITECTURE.md),
[`SECURITY.md`](SECURITY.md), [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md),
and the prompts under [`prompts/`](prompts/). v1 is the minimum
lovable version Evan can point at a single private repository
(probably **restaurhaunt** or **speakeasy**) to replace the
manual plan-files-in-the-repo orchestration loop he has been
running for the last several weeks.

The constraints, restated from the current design:

- **Two-plane.** GitHub Actions for control, Kubernetes Jobs
  (on hermetic's `ekdromos` cluster) for execution.
- **Stateless.** All state lives in GitHub (issues, PRs,
  labels, structured `🤖 hubert-…` comments, assignments).
  No local persistence on either plane.
- **Webhook-primary.** Workflows trigger on `issues`,
  `pull_request`, `check_run`, etc. A ~2-minute `schedule:`
  backstops missed webhooks and reaps stale locks.
- **Trust gate at origination.** Committers and `hubert-bot`
  only; everyone else silently ignored.
- **Hubert ships binaries + a contract.** The runner binary
  and the Go dispatcher go upstream; each deployment provides
  its own image satisfying [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md).

## No scope reduction

Any execution agent that picks up this plan: **deliver all of
v1, not the easy parts of v1.** "v1" is defined by the Tasks
list below. Each task is in scope unless explicitly moved to
"Out of scope". If a task turns out to be larger than
expected, file a sub-issue with `decomposition-depth: 1` and
link it from the parent — do not silently drop it.

## Tasks

### 1. Project skeleton

- `go mod init github.com/<owner>/hubert` (final repo path
  TBD by Evan; local working directory is currently
  `/var/home/nemequ-desktop/home/local/src/hubert/`).
- Go layout:
  - `cmd/hubert-runner/` — the in-Job runner binary.
  - `cmd/hubert-dispatch/` — the GHA-side Job submitter,
    ported from `../hermetic/docker/tools/hermes-delegate/k8s.go`.
  - `cmd/hubert-snap/` — the per-repo snapshot builder called
    from the orchestrator workflow.
  - `internal/githubapi/` — shared `go-github` wrappers:
    snapshot types, lock acquire/heartbeat/release, label
    writes, comment parsing.
  - `internal/dispatch/` — Job template + admission-policy-
    compliant spec construction.
  - `internal/orchestrator/` — parse structured action list
    from orchestrator output; action types as a tagged
    union.
  - `internal/runner/` — clone, CLI invocation, prompt
    dispatch, checkpoint-and-exit plumbing used by the
    runner binary.
  - `internal/budget/` — per-run and per-day cost tracking
    helpers.
- Embed the prompt files via `//go:embed` so the runner is
  fully self-contained.
- `Makefile` with `build`, `test`, `lint` targets. Build
  produces three static binaries under `bin/`.
- A minimal `.github/workflows/ci.yml` that builds and
  tests Hubert itself (not the tick workflow — that's task
  4).

### 2. The runner binary (`cmd/hubert-runner`)

This is the binary downstream images embed. It runs inside
each execution or reviewer Job and does:

- Read task parameters from env (`HUBERT_MODE`, `HUBERT_AGENT`,
  `HUBERT_MODEL`, `HUBERT_REPO`, `HUBERT_ISSUE` / `HUBERT_PR`,
  `HUBERT_RUN_ID`, `HUBERT_TIER`, `HUBERT_BUDGET_USD`,
  `HUBERT_WORKTREE`) per the contract in
  [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md).
- Kill-switch and trust-gate re-check against current GitHub
  state (the orchestrator already filtered, but a stale Job
  queue could fire after a kill-switch flip; the runner
  checks again before doing anything expensive).
- **Acquire the lock.** Assign `hubert-bot` to the issue (or
  post an `in-review` heartbeat comment on the PR for the
  reviewer flow). If someone else already holds it, log and
  exit 0 — the orchestrator will notice on the next pass.
- **Clone fresh** into `$HUBERT_WORKTREE`.
- **Invoke the chosen CLI** against the embedded execution
  or reviewer prompt, passing:
  - `claude --print --model <model>` (default);
  - `opencode run --dangerously-skip-permissions -m <model>`;
  - `gemini -p ... --yolo -m <model>`.
  The prompt is passed on stdin or `-p`, whichever the
  specific CLI supports cleanly. Stream stdout/stderr to the
  pod's log.
- **Heartbeat.** Every 2 minutes (configurable), post / edit
  the `🤖 hubert-run <run-id>` comment on the issue with a
  timestamp and the current tool phase. This is the liveness
  signal for stale-lock reaping.
- **Watch the budget.** Track cost via the CLI's own cost
  output (all three CLIs report `total_cost_usd` in
  `--format json` mode). If spend approaches the per-run cap,
  SIGTERM the CLI, commit whatever state exists, post an
  `🤖 hubert-escalate reason=budget` comment, exit 0.
- **On CLI exit:** commit, push a branch
  `hubert/issue-<N>-run-<run-id>`, open a PR (execution
  mode) or post the review comment + label (reviewer mode).
- **Checkpoint-and-exit escalation.** If the CLI emits a
  structured escalation marker (`need-tier: large` in the
  commit message or a final comment it wrote), or exits
  non-zero with a recognizable reason (OOM, deadline, CLI
  missing), the runner posts the corresponding structured
  escalation comment and exits 0 — the next orchestrator pass
  handles re-queueing.

The runner does NOT try to resume a previous run's session —
the checkpoint-and-exit model explicitly starts fresh from
HEAD of the branch. Session state is out of scope.

### 3. The dispatcher binary (`cmd/hubert-dispatch`)

Ported from `../hermetic/docker/tools/hermes-delegate/k8s.go`
with the label/env-var renames listed in
[`HERMETIC-HANDOFF.md`](HERMETIC-HANDOFF.md):

- Render a `batch/v1` Job + `v1` ConfigMap from a
  `text/template` embedded in the binary. Job container
  runs `hubert-runner` with env vars populated from the
  task parameters.
- Apply via `kubectl apply -f -` (subprocess; no
  client-go — see the hermetic rationale).
- Drop the GCS Fuse mount blocks; Hubert doesn't need them.
- Keep the existing resource-tier map (small / medium /
  large / xlarge) and the 6h admission ceiling.
- Support `-detach` so GHA workflows can return quickly
  after submitting.

Invoked from GHA workflows as:

```
hubert-dispatch \
    -size medium \
    -agent claude -model opus \
    -mode execution \
    -repo restaurhaunt -issue 42 \
    -budget-usd 5 \
    -run-id <generated>
```

### 4. The snapshot helper (`cmd/hubert-snap`)

Called by the orchestrator workflow to build the per-repo JSON
snapshot the orchestrator prompt consumes. Reads:

- All open issues with author, labels, assignees, and the
  most recent ~5 `🤖 hubert-…` structured comments on each.
- All open PRs with author, labels, head/base, mergeable
  state, CI status, and the most recent few review comments.
- The current collaborator list.
- The kill-switch issue state.
- Today's running cost total for this repo (summed from
  `🤖 hubert-cost` comments on the kill-switch issue or
  on the individual issues, per whatever audit-log shape
  wins in v1).

Emits a single JSON document on stdout. The orchestrator
workflow pipes it to `claude --print` as user input.

### 5. GHA workflow templates

Workflows go in each watched repo's `.github/workflows/`.
Ship them as a copy-and-paste template plus a documented
`inputs:` contract; generating them from Hubert is out of
scope for v1. Templates:

- `hubert-orchestrator.yml` — triggers: `issues`,
  `issue_comment`, `pull_request`, `pull_request_review`,
  `check_run`, `schedule: "*/2 * * * *"`,
  `workflow_dispatch`. Steps: check kill switch; run
  `hubert-snap`; feed to `claude --print`; parse action
  list; for each action, either act inline (label / comment
  / noop) or shell out to `hubert-dispatch`.
- `hubert-ci.yml` — the minimum CI pass (lint + fast tests)
  the reviewer agent treats as ground truth. Single stage in
  v1; the hooks for label-gated expensive stages are laid in
  but no additional stage actually ships.

All GHA-side binaries (`hubert-snap`, `hubert-dispatch`,
`claude`, `gh`) are expected on the GHA runner's `$PATH` via
an install step at the top of each workflow.

### 6. Per-repo config (`.hubert/README.md`)

Loosely-parsed markdown with headings per
[`.hubert/README.md.example`](.hubert/README.md.example):

- Build / test / lint commands.
- Per-issue and per-day cost caps (override global
  defaults).
- Optional "allowed backends" list — a privacy-sensitive
  project can refuse routing to anything except `claude`
  even if the deployment image has all three CLIs
  installed.
- Free-form notes the orchestrator reads as context.

The orchestrator reads this verbatim as part of its prompt
input; no structured parser in v1.

### 7. Kill switch

Implemented entirely through GitHub state. Conventions:

- Each deployment designates a kill-switch issue (typically
  `hubert-bot/hubert-config#1` or per-repo `.hubert-stop`).
- Open + labeled `STOP` → Hubert workflows exit at the top
  before touching any LLM.
- The orchestrator workflow's first step is a `gh` call to
  check this; no token spend if it's engaged.
- The runner binary re-checks on startup, since a Job
  queued before the flip can still fire after.

### 8. Cost tracking and per-day cap

- Each runner exit posts a `🤖 hubert-cost <run-id> <USD>`
  comment on the issue with the total spend for that run.
- `hubert-snap` sums today's `🤖 hubert-cost` comments per
  repo into the `daily_spend` field on the snapshot.
- The orchestrator prompt is told `daily_spend` and the cap;
  it refuses to emit `dispatch-*` actions that would push
  over.
- The orchestrator workflow cross-checks the parsed action
  list against the same cap before dispatching — defensive
  backstop against a confused orchestrator.

### 9. Structured logging

Both binaries log JSON lines to stderr with `run_id`,
`phase`, `elapsed_ms`, `repo`, `issue` fields. GHA workflows
`| jq` the orchestrator-pass output into a structured
summary the workflow run page renders legibly. Errors
include a stack trace; nominal operation gets one line per
phase transition.

### 10. End-to-end test against a throwaway repo

Before declaring v1 done:

- Stand up a throwaway private repo; add `hubert-bot` as a
  collaborator.
- Deploy the workflow templates to that repo; populate
  required GHA secrets.
- Build the reference image from the hermetic Dockerfile
  with `hubert-runner` added; push to `ghcr.io/nemequ/hubert`.
- File a trivial issue ("add a hello world function in
  `hello.go`"). Verify the entire loop: orchestrator
  dispatches execution Job → Job opens PR → CI runs →
  orchestrator dispatches reviewer Job → reviewer merges.
- File a scope-creep-bait issue; verify the reviewer
  rejects it and the iterate path works.
- Simulate failure: `kubectl delete job/<running-run>`
  mid-flight. Verify the next orchestrator pass reaps and
  re-queues.
- Simulate kill switch: flip the label; observe zero LLM
  calls on the next tick.

## Verification

- `make build && make test && make lint` all pass.
- `hubert-snap` dry-run against a real repo prints a sane
  snapshot JSON.
- Orchestrator pass on that snapshot prints a reasonable
  action list (no dispatches actually fire; use a
  `--dry-run` flag on `hubert-dispatch`).
- End-to-end test above succeeds against the throwaway
  repo.
- Kill switch demonstrably zero-cost: flip, tick, observe
  no LLM invocations.
- Stale-lock reap recovers from simulated crash without
  human intervention.

## Out of scope for v1

Explicit non-goals:

- **Policy engine for fine-grained model/tier selection.**
  v1 has `agent` and `model` in the action schema, but the
  orchestrator's routing policy is whatever the orchestrator
  prompt says — no label taxonomy, no per-project backend
  allowlist beyond simple "allowed backends" in
  `.hubert/README.md`. The label-based policy engine is a
  named v2 target.
- **Multi-repo cost aggregation.** v1 tracks per-repo only.
- **`hubert-log` dedicated audit repo / Discussion.** v1
  puts cost comments on the per-repo kill-switch issue.
- **GitHub App** instead of PAT. v2.
- **Attachment classifier / "lift" mechanism.** v3 when the
  public-repo flow matters.
- **Multi-stage CI pipeline.** v1 ships lint + fast tests
  only; the hooks are there for more but the stages aren't.
- **A web UI of any kind.**
- **Resuming a prior agent session across Jobs.** Explicitly
  rejected — the checkpoint-and-exit model starts fresh.

## v2 / v3 (notes only, not in scope)

**v2** adds the pieces that make Hubert pleasant beyond a
single private repo:

- GitHub App migration (replaces PAT, fixes attribution).
- Per-org and per-team auto-approve rules.
- Cross-repo cost aggregation and global caps.
- Dedicated `hubert-log` audit repo or GitHub Discussion.
- Label-driven policy engine for backend/model/tier
  selection. Taxonomy suggestion:
  `complexity:trivial|moderate|heavy`,
  `area:frontend|backend|infra`,
  `needs:web-search|reasoning|privacy`. The orchestrator
  consumes these as policy inputs. See
  [`ARCHITECTURE.md`](ARCHITECTURE.md) "Why three CLIs" for
  the motivation; v2 turns this from a free-form prompt
  concern into a testable label-driven routing table.
- Expensive CI stages gated by labels + per-PR budget cap.

**v3** makes the open-source bot use case real:

- Attachment-content classifier for the pidifool PDF flow.
- The "lift" mechanism: a committer can comment
  `@hubert-bot lift` on an untrusted-author issue to
  promote it.
- Per-contributor reputation tracking.
- Default-deny-after-N-hours for stale lifted issues.
