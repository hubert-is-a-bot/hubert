# Hubert

> **Status:** research-phase design. No code yet. This is the
> single canonical plan for Hubert — architecture, security
> model, image contract, v1 implementation plan, and reference
> deployment notes, all in one document. Every section header
> carries a **horizon tier** (`[Now]` / `[Soon]` / `[Later]` /
> `[Someday]`) — see § 0 below for what the tags mean. The
> only other load-bearing files in this repo are the three
> prompts in [`prompts/`](prompts/) (load-bearing artifacts,
> invoked by path at runtime) and the per-repo config example
> at [`.hubert/README.md.example`](.hubert/README.md.example).

---

## 0. How to read this document: horizon tiers

Hubert is a long-lived design sketch. Some of it is concrete
and intended to execute against in the next increment; some
of it is deliberately aspirational and may change
substantially on contact with reality. The architecture below
spans both, so every section header carries a tier tag that
tells you how to read it.

- **[Now]** — next implementation increment. Concrete,
  scoped, intended to execute against. Changes here need
  justification.
- **[Soon]** — earned by *Now* landing. Design is firm but
  subject to revision based on *Now*-learnings. Treat as
  "planned v1.5."
- **[Later]** — directions committed to, not yet designed
  in detail. Shape is sketched; specifics will be revisited
  once the basic loop has taught us something. Treat as
  "v2 material."
- **[Someday]** — vision sketches. Expect significant
  change on contact. Captured here so they don't get
  forgotten, not because they're next. Treat as
  "v3 and beyond."

Two framings that follow from the tiers:

- **Growth is encouraged in the later tiers, not the
  earlier ones.** Ideas that belong in *Later* or *Someday*
  should be captured even when half-formed — losing them
  is worse than carrying them speculatively. Ideas that
  belong in *Now* should be pressured hard before they're
  added, because *Now* is a commitment.
- **Promotion, not addition, is the normal path from
  *Later* → *Soon* → *Now*.** An idea becomes ready for
  the next tier because experience earned it that
  promotion. A new idea almost always starts in *Later*
  or *Someday*; only architectural blockers land in *Now*
  directly.

The tiers also appear in the TOC so the confidence gradient
is visible from the outline alone.

---

## Table of contents

0. [How to read this document: horizon tiers](#0-how-to-read-this-document-horizon-tiers) — framing
1. [Summary](#1-summary) — framing
2. [What Hubert is / is not](#2-what-hubert-is--is-not) — framing
3. [Architecture](#3-architecture) — **[Now]** (the core design is Now; individual decisions below split by tier)
   - [Two-plane overview](#two-plane-overview) — **[Now]**
   - [The five components](#the-five-components) — **[Now]**
   - [Design decisions](#design-decisions) — mixed; tagged per-decision
4. [Trust and security](#4-trust-and-security) — **[Now]**
5. [Image contract](#5-image-contract) — **[Now]**
6. [v1 implementation plan](#6-v1-implementation-plan) — **[Now]** and **[Soon]** bands below
   - [[Now] first-increment tasks](#now-first-increment-tasks)
   - [[Soon] tasks earned by the Now loop landing](#soon-tasks-earned-by-the-now-loop-landing)
7. [Reference deployment: hermetic](#7-reference-deployment-hermetic) — **[Now]** (operator context)
8. [v2 / v3 roadmap](#8-v2--v3-roadmap) — **[Later]** and **[Someday]**
   - [v2: per-project agent personas](#v2-per-project-agent-personas) — **[Later]**
   - [v2: remaining items](#v2-remaining-items) — **[Later]**
   - [v2: central scheduler with reaction-based signaling](#v2-central-scheduler-with-reaction-based-signaling) — **[Later]**
   - [v3: the open-source bot use case](#v3-the-open-source-bot-use-case) — **[Someday]**
9. [Installing Hubert (planned)](#9-installing-hubert-planned) — **[Soon]**
10. [Open design threads](#10-open-design-threads) — mixed; tagged per-thread
11. [What v1 explicitly defers](#11-what-v1-explicitly-defers) — canonical deferrals index
- [Appendix A: Building v1 — the multi-agent work plan](#appendix-a-building-v1--the-multi-agent-work-plan) — meta-plan for the human + agents building Hubert

---

## 1. Summary — framing

Hubert is a small two-plane tool for AI-driven development on
GitHub. You point it at a repository, you file an issue, and
on the next webhook (or ~2-minute `schedule:` backstop) Hubert
reads the issue, dispatches an execution agent on a Kubernetes
Job to implement it, dispatches a separate reviewer agent on
another Job to check the resulting pull request, and either
merges the PR or asks the implementer to iterate. The work
humans normally do in a GitHub project — file issues, plan
work, implement, review, merge — is done by AI agents
coordinating through GitHub itself.

The two planes:

- **Control plane:** short-lived GitHub Actions workflows in
  each watched repo. Webhook-triggered for reactive work,
  with a ~2-minute `schedule:` backstop. Runs a one-shot
  Claude Code orchestrator pass that outputs a structured
  action list; the workflow either acts inline (label,
  comment, noop) or dispatches a Kubernetes Job.
- **Execution plane:** Kubernetes Jobs on the deployment's
  cluster. Each Job runs the Hubert runner binary, which
  invokes the orchestrator-selected LLM CLI (`claude`,
  `opencode`, or `gemini`) against an embedded execution
  or reviewer prompt, does the work in a fresh clone of
  the repo, pushes a branch, and posts a structured comment
  back to GitHub.

The two planes share nothing but GitHub state. GitHub is the
queue, the lock, the audit log, the event bus, and the UI.

---

## 2. What Hubert is / is not — framing

### Is

- **A two-plane system**, GHA + K8s Jobs, coordinating
  through GitHub.
- **Three Go binaries** shipped upstream: `hubert-runner`
  (in-Job LLM invocation + lock/heartbeat/escalation),
  `hubert-dispatch` (GHA-side Job submitter),
  `hubert-snap` (per-repo state snapshot builder).
- **Three prompts** in [`prompts/`](prompts/):
  [`orchestrator.md`](prompts/orchestrator.md) decides
  what to do, [`execution.md`](prompts/execution.md)
  does it, [`reviewer.md`](prompts/reviewer.md) checks
  it. These are the load-bearing files; the binaries are
  plumbing.
- **Binary-ships-upstream, image-ships-per-deployment.**
  Each deployment provides its own container image
  satisfying the contract in section [5](#5-image-contract).
- **A trust amplifier.** Inherits the trust posture of the
  underlying GitHub repo; adds nothing.

### Is not

- **Not a daemon.** Each control-plane tick is a fresh GHA
  run; each execution-plane Job is a fresh pod. No
  long-running process, no local state.
- **Not a merge bot for human-authored PRs.** Hubert
  reviews and merges only PRs that Hubert itself opened,
  in response to Hubert-trusted issues.
- **Not a hosted service.** One user's personal
  automation, on credentials and cluster capacity that
  user controls.
- **Not a security boundary against compromised
  committers.** Trust model is GitHub's; Hubert adds
  nothing.
- **Not a container image.** Upstream ships Go binaries,
  prompts, and workflow templates only.
- **Not a sandbox on its own.** The deployment's
  admission policy and cluster posture provide isolation;
  Hubert consumes those, doesn't provide them.

---

## 3. Architecture — **[Now]**

### Two-plane overview

The system separates *deciding what to do* from *doing it*.

```
┌───────────────────────────────────────────────────────────┐
│ CONTROL PLANE — short-lived GHA workflow runs              │
│                                                            │
│   trigger: webhook (issues, PRs, check_runs) + schedule    │
│      ↓                                                     │
│   kill-switch gate  →  hubert-snap  →  claude --print      │
│   (gh)                 (per-repo      (orchestrator        │
│                         state JSON)     prompt)            │
│      ↓                                                     │
│   parse action list  →  inline actions (label / comment)   │
│                      →  hubert-dispatch (submit K8s Job)   │
└──────────────────────────────┬────────────────────────────┘
                               │ kubectl apply
                               ▼
┌───────────────────────────────────────────────────────────┐
│ EXECUTION PLANE — one K8s Job per execution/reviewer run   │
│                                                            │
│   hubert-runner                                            │
│      ↓                                                     │
│   kill-switch / trust re-check                             │
│      ↓                                                     │
│   acquire lock (gh assign + heartbeat comment)             │
│      ↓                                                     │
│   git clone → invoke CLI (claude / opencode / gemini)      │
│      ↓                                                     │
│   heartbeat every 2 min (edits structured comment)         │
│      ↓                                                     │
│   push branch + open PR / post review + label              │
│      ↓                                                     │
│   on escalation: commit what you have, post escalation     │
│      comment, exit 0 — next orchestrator pass re-queues    │
└───────────────────────────────────────────────────────────┘
                               │
                               │ git push / PR events / comments
                               ▼
                    (back to the control plane as
                     new webhook-triggered workflow runs)
```

A pod's `git push` fires a webhook that re-enters the control
plane on the next orchestrator run. Execution agents never
block on each other — coordination is through GitHub state,
not process supervision.

### The five components

1. **Control-plane workflows (GHA).** A small set of workflow
   YAMLs in `.github/workflows/` in each watched repo.
   Triggers: `issues`, `issue_comment`, `pull_request`,
   `pull_request_review`, `check_run`, `schedule` (~2 min),
   `workflow_dispatch`. Each workflow is cheap: check the
   kill switch, build the per-repo GitHub state snapshot,
   run the orchestrator pass, and either act inline (label,
   comment, `noop`) or invoke `hubert-dispatch` to submit
   execution/reviewer Jobs on the cluster. No heavy work
   runs inside GHA itself.

2. **Orchestrator pass.** A `claude --print` invocation
   against `prompts/orchestrator.md`, fed the per-repo
   snapshot. Planning-only, no Bash/Edit tools. Outputs a
   structured action list:
   `dispatch-execution(issue=N, mode=M, iteration=I, agent=A, model=Mo, tier=T)`,
   `dispatch-reviewer(pr=P, agent=A, model=Mo, tier=T)`,
   `reap-stale-lock(issue=N, run_id=X)`,
   `escalate(issue=N, reason=…)`, `noop`. Short, cheap,
   side-effect-free except for token spend and any
   label/comment writes the workflow applies on its behalf.
   Always runs on Claude Code — planning quality matters
   and the pass is short; only execution/reviewer work
   gets routed to cheaper backends.

3. **Execution agent.** The Hubert **runner binary**
   (`cmd/hubert-runner`) running inside a Kubernetes Job
   submitted via the deployment's admission-policy-
   enforced template. The runner clones the repo, acquires
   the GitHub lock (assignment + heartbeat comment),
   invokes the orchestrator-selected LLM CLI with the
   embedded execution prompt, pushes a branch, releases
   the lock, and exits. If it hits a constraint it can't
   solve at its current tier (memory, model quality,
   time), it commits what it has, posts a structured
   escalation comment, and exits 0 for the next
   orchestrator pass to re-queue.

4. **Reviewer agent.** Same Job shape as the executor,
   typically a smaller tier. Reads a PR, evaluates it for
   quality / completeness / scope-fidelity, reads the CI
   status checks, posts a review comment, and either
   merges (on approval) or labels the PR
   `hubert-changes-requested`. Separate process from the
   executor with fresh context — a real second-opinion
   pattern, not a self-review rubber stamp.

5. **GitHub.** The state store, the lock manager, the
   queue, the event bus (via webhooks), the audit log,
   and the user-facing UI. Hubert owns nothing GitHub
   doesn't already own.

The deployment's Kubernetes infrastructure (Job template,
admission policy, namespaced RBAC, Workload Identity) is a
substrate execution and reviewer agents run *on*, not a
component Hubert implements from scratch. The reference
deployment is detailed in section
[7](#7-reference-deployment-hermetic).

### Design decisions

Each decision below records the *why* so future judgment calls
on edge cases can reference the original reasoning.

#### Why two planes (GHA control + K8s execution)

The first sketch was a single stateless Go binary that ran on
cron ticks and spawned subprocesses for execution. Two follow-
up conversations pulled the shape apart along the natural
seam: *deciding* what to do is cheap, fast, webhook-reactive
work; *doing* the work is expensive, resource-heavy, slow
work.

- **Control plane wants low-latency event triggers.** Webhooks
  fire on `issue.opened`, `pull_request.ready_for_review`,
  `check_run.completed` the moment those events happen.
  Strictly better than a 15-minute poll.
- **Execution plane wants resource isolation and custom
  tiers.** GHA-hosted runners are fixed-size (7GB memory, 2
  vCPU free-tier, 6h max). K8s Jobs on the reference cluster
  give us admission-policy-enforced tiers (small / medium /
  large / xlarge, up to 32Gi and 6h).
- **The two planes don't need to share a runtime.** They
  communicate exclusively through GitHub state — commits,
  PRs, comments, labels, status checks. An executor's
  `git push` fires a webhook that re-enters the control
  plane on the next orchestrator pass. Same mechanism the
  bot already uses for humans.

The cost is tighter coupling to two vendors (GHA for
webhooks, whatever K8s cluster for execution). The
mitigation is that both are operated by someone else and the
execution plane uses vanilla K8s primitives (Jobs,
ConfigMaps, emptyDir) — no App Engine, no Pub/Sub — so it
ports to any K8s cluster.

#### Why GitHub Actions as the control plane

Alternatives considered and rejected: cron/systemd on a
laptop or VPS (would own our own supervision tree, polling
window is coarser than webhooks); GAE + Pub/Sub (more
GCP-specific surface for something GHA does with no
additional infrastructure); `actions-runner-controller` on
the same cluster (another controller to operate; only
matters if GHA minutes become a real constraint).

GHA wins for the control plane because:

- Webhook-to-workflow trigger mapping is the thing we'd
  otherwise have to build.
- `GITHUB_TOKEN` is auto-provisioned with per-repo scope;
  no PAT to manage for control-plane calls.
- The `concurrency:` keyword handles dedup of overlapping
  event fan-out natively.
- The workflow YAML is *also* where CI quality checks live,
  so the control plane and the quality plane share a config
  substrate.
- Free-tier minutes are generous for short orchestrator
  passes; we pay the real compute bill on K8s, not GHA.

The known weak spot: GHA's `schedule:` trigger is unreliable
(delays of 15+ minutes and occasional silent skips). The
mitigation: the schedule is a *backstop*, not the primary
trigger. Webhook events fire on every real state change; the
scheduled sweep only catches stale locks and missed webhook
deliveries. A 15-min delay in reaping is not user-visible; a
15-min delay in picking up a new issue would be, and
webhooks solve that directly.

#### Why ~2 minute scheduled ticks

Each scheduled tick is a short orchestrator pass (tens of
seconds, no source checkout, no heavy model calls). Most
ticks are no-ops. GHA free-tier minutes are plenty for
2-minute ticks across the repos Hubert will realistically
watch. Tighter ticks mean faster recovery from missed
webhooks and faster reaping of dead Jobs at negligible cost.

#### Why K8s Jobs instead of GHA-hosted runners for execution

- **Resource tiers.** The reference deployment defines
  small / medium / large / xlarge tiers (1Gi–32Gi memory,
  1h–6h deadlines) with a `ValidatingAdmissionPolicy`
  enforcing ceilings. GHA-hosted runners are one fixed size
  on the free tier.
- **Isolation.** Each K8s Job runs in its own pod with
  namespaced RBAC, an emptyDir filesystem, and no cross-pod
  state. Admission policy rejects `hostNetwork`, `hostPID`,
  or privileged containers.
- **Independent killability.** A misbehaving executor is a
  `kubectl delete job/<name>` away from stopped. In the
  GHA-hosted model, killing a runner means canceling the
  workflow run, which also kills the control-plane loop
  that submitted it.
- **The work already exists.** The reference deployment
  (hermetic) has the Job submission binary, admission
  policy, RBAC, and Workload Identity wiring. Hubert ports
  it with label renames.

Ephemeral working trees fall out of this directly: each Job
gets a fresh clone into its pod's emptyDir volume; the tree
dies with the pod. Nothing to clean up between runs; no
shared state to corrupt.

#### Why checkpoint-and-exit for escalation

An execution agent that finds it needs more resources could
block and wait, spawn a bigger child, or make a synchronous
RPC request. All of those couple the two planes.

**Checkpoint-and-exit:** the agent commits whatever work it
has, pushes, posts a structured comment naming the
constraint (`need-tier: large`, `need-model: opus`, or
free-form reason), and exits 0. The next orchestrator pass
reads the comment, queues a fresh Job at the requested tier,
and the new Job picks up at HEAD of the branch with the
comment trail as continuation context.

This falls out of the design:

- Reuses the same communication channel every other piece
  of the system uses (`git push` + comment).
- No nested-job deadlocks — nothing ever waits on anything
  it submitted.
- Handles voluntary ("I can tell this is too hard for
  Sonnet") and involuntary (OOMkilled, deadline exceeded)
  escalation uniformly. For involuntary, the orchestrator
  sees `kubectl get job` status and treats the failure as
  implicit escalation.
- Composes with the trust model: the escalation comment is
  from `hubert-is-a-bot`, which the origination trust check
  already treats as trusted.

Voluntary is more reliable than involuntary — OOM is hard to
checkpoint *from* (by the time you're killed you may not
have room to `git push`). Pair with conservative up-front
tier selection; treat OOM-as-implicit-escalation as the
fallback, not the primary path.

The per-issue **escalation budget** caps recursive
promotion: an agent that keeps asking for more, gets
promoted to xlarge+opus, and still can't make progress gets
kicked up to a human rather than escalated again.

The runner does NOT try to resume a previous run's LLM
session across Jobs — checkpoint-and-exit explicitly starts
fresh from HEAD of the branch. Escalation changes the tier
and model; the new model shouldn't inherit the old one's
context. Reviewer fresh-context is a *feature*, not an
oversight.

#### Why three prompts, not one

- **Different jobs, different context budgets.** The
  orchestrator needs GitHub state across all in-flight work
  but no source code. The execution agent needs source code
  and the issue but no other in-flight work. The reviewer
  needs the PR diff and the issue but no planning history.
- **Independent failure modes.** An execution that OOMs or
  rate-limits doesn't take down the orchestrator.
- **Conflict-of-interest separation for review.** The
  reviewer has fresh context, untainted by the planning
  that produced the PR. A different process looking at the
  diff with no memory of what the executor was trying to do
  — closer to code review than self-review.
- **Parallelism on K8s.** Each execution and reviewer is
  its own Job/pod with its own log stream. Orchestrator
  passes stay small and fast.

#### Why three CLIs, not one

The orchestrator chooses a backend per task: `agent=claude`,
`agent=opencode`, or `agent=gemini`. The runner invokes
whichever was chosen with the model specified in the same
action (`model=opus`, `model=opencode/big-pickle`,
`model=gemini-2.5-pro`, etc.).

Multiple backends pay off because each has a different
cost/capability curve:

- **Claude Code** — strongest on complex refactors and
  design; best-tested tool loop; also the most expensive.
  Default for execution work that touches multiple files or
  needs real reasoning.
- **OpenCode** — unlocks OpenAI models and free tiers
  (BigPickle, Nemotron, codex-mini) that are capable-enough
  for a lot of leaf work at zero or near-zero cost. Good
  fit for small scoped changes, doc updates, test
  scaffolding, reviewer passes on simple PRs.
- **Gemini** — has Google Search built in. Dispatched when
  a task needs to verify an API surface, look up a library
  version, or cross-check an error message against upstream
  issues.

The runner keeps the backends interchangeable by passing
prompts verbatim — the execution and reviewer prompts
describe goals and file paths rather than any specific
tool-call syntax, so they work on whichever CLI got
dispatched. This portability is a *constraint on prompts*,
not a runtime feature: if a prompt starts reaching for
Claude-specific tools, it has to be reworked or the
orchestrator has to be prevented from routing that task to
non-Claude backends.

Policy lives in two places: the orchestrator prompt (which
picks `agent`/`model`/`tier` per task, using issue labels
and `.hubert/README.md` as hints) and the per-project
`.hubert/README.md` (which may pin `allowed_backends` for
privacy- or quality-sensitive projects). v2 formalizes the
orchestrator's choice into a testable label-driven routing
table.

#### Why GitHub-as-state, with no local persistence

The two-plane split makes this constraint load-bearing: the
control plane and execution plane share no filesystem, no
database, no memory. The only substrate both can read and
write is GitHub itself.

Even absent the constraint, GitHub-as-state has properties
the alternatives don't:

- Durable across binary reinstalls, cluster moves, and
  full backup loss.
- Human-inspectable in the GitHub UI without any tooling.
- Already the thing the user looks at when thinking about
  the project — no second source of truth to keep in sync.
- Same locking mechanism works identically whether two
  ticks are racing on the same runner or on opposite sides
  of the planet.

The cost: every state read is a GitHub API call. At the
scheduled tick rate (~720/day/repo at 2-minute intervals)
against a 5k/hr authenticated rate limit, we're well inside
budget. Webhook-triggered workflows don't add to poll
load — they *replace* polling for reactive events.

#### Why GitHub locking via assignment + heartbeat

Locking is the part of stateless distributed systems that's
usually hard. Here it's free:

- **Atomic assignment.** `POST /repos/…/issues/:n/assignees`
  is the lock acquisition. Two Jobs racing to start the
  same issue don't actually race — whichever hits GitHub
  first wins; the other's `gh api` call sees the
  assignment in place and exits cleanly.
- **Heartbeat as liveness.** The runner updates its
  `🤖 hubert-run <run-id>` comment every ~2 minutes. Stale
  heartbeat → dead execution → reapable. `kubectl get job`
  is a second liveness signal for Jobs the orchestrator
  submitted.
- **Comment trail as audit log.** Every lock event is a
  comment, visible to humans, queryable via API, free.

Reaping on each scheduled tick is the analogue of "the
lease expired, garbage collect" in a traditional locking
service — just that the lease expiration is tracked in
comment timestamps and pod status, not a Redis key.

#### Why GitHub-account trust at origination

The original brief required a human-in-the-loop checkpoint
at merge. Working through what "human in the loop" actually
means, the load-bearing property is "a human Hubert trusts
initiated this work" — that's an *origination* property,
not a *merge* property. If Evan opens an issue, the entire
downstream chain (plan → implement → review → merge) is
trust-rooted in his original action. If a stranger opens an
issue, no amount of Hubert review at the end can change the
fact that the work originated with someone Hubert shouldn't
trust.

Trust gate:

```
trusted(issue) :=
    issue.author == "hubert-is-a-bot"
    OR issue.author has commit/admin access to issue.repository
```

Anything not in that set is ignored — silently. No comment,
no label, no token spend, no acknowledgement. **Silence is
the cheapest and least-attackable response.** An attacker
who files an issue and gets no response can't even confirm
Hubert is watching the repo.

There is intentionally no "warn the author politely" path.
That path is a token-spend vector and an information leak.
The v2 **lift** mechanism is a controlled way to bring
untrusted issues into the actionable set, but only via
explicit committer action.

This makes Hubert a *trust amplifier*: it inherits the
trust posture of the underlying GitHub repo and does
nothing to add or subtract from it.

#### Why reviewer can merge its own side's work

Follows from trust-at-origination. The reviewer is not
"approving an arbitrary diff" — it's approving a diff
produced by a chain of Hubert-internal agents acting on
behalf of an originally-trusted human. The conflict of
interest in self-merge is real for agents that *originate*
work, not for agents that *implement* trusted requests.

The reviewer is also a separate process from the executor
with fresh context, which is the structural defense
against sloppy self-review. It's a different agent looking
at the diff with no memory of what the executor was trying
to do.

#### Why a dedicated `hubert-is-a-bot` account with a PAT for execution

- A dedicated account means commits and comments are
  clearly attributed to Hubert, not to Evan. When
  something goes wrong, the audit trail is unambiguous.
- Inside GHA workflows, the ambient `GITHUB_TOKEN` handles
  most control-plane operations (reading state, adding
  labels, posting comments). No PAT needed there.
- The execution plane runs outside GHA, so K8s Jobs
  authenticate to GitHub with a PAT stored as a K8s
  Secret. This is also what lets executor pushes trigger
  new GHA workflow runs — `GITHUB_TOKEN`-authored pushes
  are blocked from triggering downstream workflows by
  default (anti-loop), but PAT-authored pushes do trigger
  them, which is exactly what we want.
- A GitHub App is the cleaner long-term answer (per-repo
  installation, fine-grained permissions, no user-token
  expiry) and is the v2 upgrade path.

#### Why Hubert ships binaries, not an image

Hubert upstream ships **executables and a contract**, not a
container image. Each deployment provides its own image
that satisfies the contract (section [5](#5-image-contract)).

The image is the deployment-specific layer: base image, LLM
CLI set, credential layout, registry, build pipeline — all
operator choices. Shipping an image upstream would either
force those choices on operators or bloat the image with
everything every operator might want.

The contract is small: the image must have `hubert-runner`,
`git`, `gh`, and at least one supported LLM CLI on `$PATH`;
the Job gets a documented set of env vars and a writable
scratch mount; the runner reads its prompts from embedded
strings, so no prompt-file mount is needed. Everything
else — base image, credential mounting, registry, build
pipeline — is up to the deployment.

#### Why Go for what little code we write

Three binaries, all Go:

- **Runner** (`cmd/hubert-runner`). Runs inside each
  execution or reviewer Job. Clones the repo, acquires the
  lock, invokes the LLM CLI, enforces the per-run budget,
  pushes a branch, posts outcomes. The binary downstream
  images must contain.
- **Dispatcher** (`cmd/hubert-dispatch`). Called from GHA
  workflows to submit Jobs. Renders a `batch/v1` Job + `v1`
  ConfigMap from an embedded `text/template`, applies via
  `kubectl`. Ported from the reference deployment's
  `k8s.go`.
- **Snapshot helper** (`cmd/hubert-snap`). Called from the
  orchestrator workflow to build the per-repo JSON
  snapshot (open issues, open PRs, recent `🤖 hubert-…`
  comments, collaborator list, kill-switch state,
  `daily_spend`) that the orchestrator prompt consumes.

Go wins on: matches Evan's backend stack; `go-github` is
mature; the reference deployment's submission logic is
already Go; static binaries keep the downstream image small.

`kubectl` shell-out over `client-go`: hermes-delegate ships
as a 5.2MB static binary; the same code with `client-go` is
closer to 30MB. The binary will live in a container image
so size matters less, but the dependency weight of
client-go is non-trivial for glue code. Stick with
`os/exec` + `kubectl` until there's a specific reason not
to.

#### Why per-repo config in `.hubert/README.md`, not YAML

For v1, per-repo config has at most six knobs: build/test/
lint commands, cost caps, `allowed_backends`,
`default_model`, merge style, branch pattern, and free-form
project notes for the orchestrator to read. That's a
markdown file with headings, not a structured document. A
loosely-parsed `.hubert/README.md` is friendlier to humans
(it's also a place to write contributor guidance) and
friendlier to the orchestrator (which reads it as raw
text).

The trusted-user list is *not* a config knob — it's
derived from the repo's actual GitHub collaborators. Using
commit access as the trust signal is the whole point.

When a second non-textual knob shows up that's hard to
express as prose, graduate to YAML. Until then, prose.

See [`.hubert/README.md.example`](.hubert/README.md.example)
for the full template.

#### Why the orchestrator is backend-portable, not pinned to Claude

A previous draft hard-wired the orchestrator pass to
`claude --print`. That creates an Anthropic-outage
single-point-of-stall: a rate-limit or provider incident on
Anthropic freezes the whole control plane even when OpenCode
and Gemini are healthy.

The orchestrator prompt is deliberately backend-agnostic —
same as the execution and reviewer prompts — and the
orchestrator workflow selects its backend via the same CLI
adapter the runner uses. v1 default is Claude on `sonnet` for
the pass (cheap, strong enough for planning, fast); fallbacks
to OpenCode on a capable free-tier model are triggered on
Anthropic 429/5xx responses or a sustained failure of the
primary backend over the last N ticks.

This reframes the CLI abstraction layer as Hubert's **primary
portability surface**, not just an execution-plane convenience.
Orchestrator, execution, and reviewer all consume the same
`internal/cliadapter/` interface; switching backends is a
one-field change in the Job spec or a one-call change in the
workflow.

#### Why recovery is a first-class flow, not a failure mode

The original checkpoint-and-exit design treated "I ran out of
cost / tier / memory" as escalation — the next orchestrator
pass dispatches a bigger Job. That's right for tier and model,
but wrong as the only shape: a run that hit its cost cap, got
OOMkilled, exceeded its deadline, or got rate-limited should
not reliably retry at the *same* size against the *same*
provider. That path loops without progress.

v1's recovery path is structured:

- **Cost overshoot** (mid-tool-call) → escalate with
  `need-backend: cheaper` → next dispatch goes to an OpenCode
  free pool (BigPickle, Nemotron, qwen-coder, Hermes) at the
  same or smaller tier.
- **OOM / deadline-exceeded** → escalate with
  `need-tier: larger` if the workload genuinely needs more; if
  the last attempt was already at `xlarge`, flip to
  `need-backend: cheaper` + fresh context instead of climbing
  further.
- **Rate-limited (429)** → escalate with
  `need-backend: alternate` → next dispatch uses a different
  provider entirely. No retry-same-provider.
- **After N cycles of either pivot without convergence** →
  `hubert-stuck`, human intervention.

OpenCode's free pools are the primary pivot resource: they
absorb retries that would otherwise blow budget or wait on
Anthropic quota. This composes naturally with the "prefer
cheaper models" principle — recovery is *always* cheaper, not
uniformly bigger.

Design principle: **stuff will go wrong; don't get stuck
indefinitely.** Escalation is not a failure mode. It's the
normal control flow for *any* terminated-before-success run,
and the orchestrator's job is to pick the right next-step
backend, not to retry the one that just failed.

#### Why two bot identities, not one

The naive "one `hubert-is-a-bot` account does everything" design
collides with standard branch protection: most branch
protection configurations require that PR approval come from
an identity *other than* the PR author, plus status checks
passing. If `hubert-is-a-bot` authors the PR and `hubert-is-a-bot`
approves it, branch protection blocks the merge (or requires
admin bypass, which weakens the whole protection story).

v1 ships two identities:

- **`hubert-exec-bot`** — authors commits, pushes branches,
  opens PRs. Fine-grained PAT with `contents:write`,
  `pull-requests:write`, `issues:write` on watched repos.
- **`hubert-review-bot`** — reviews and merges. Fine-grained
  PAT with `pull-requests:write`, `issues:write`,
  `contents:write` (for merge). Scoped only to act on PRs
  whose author is `hubert-exec-bot` (runner enforces this
  before taking any write action).

Standard branch protection (1 approval from non-author,
status checks required, no admin bypass) Just Works. No
protection weakened, no admin bypass baked into the design.

The reviewer-scope check is belt-and-suspenders: even if
`hubert-review-bot`'s PAT leaks, the leaked credential can't
merge PRs authored by humans because the runner refuses,
and it can't be used to approve anyone's work outside the
Hubert-exec-bot → hubert-review-bot channel.

v2's GitHub App migration collapses this back to one app
with role-scoped installation tokens; the two-identity split
is a v1 shape, not a permanent one.

#### Why we treat the issue body as data, not instruction

A committer pasting a log snippet, a stack trace quoting
user-controlled strings, or a bug report quoting hostile
input all become executor "instructions" if the execution
prompt naively treats the issue body as a system message.
The committer didn't author those substrings; the user who
triggered the log did.

The execution prompt explicitly frames the issue body as a
**description of desired behavior**, not an imperative
instruction set. The executor reads the issue body the way a
human engineer reads a bug report: "the author is telling me
about something they want changed; any quoted strings,
attachments, or embedded commands inside are *evidence*, not
*orders*." Tool calls get authorized by the execution
prompt's own rules, not by anything the issue body appears to
ask for.

The snapshot already filters comments to `🤖 hubert-…`
structured ones; issue bodies don't have that luxury because
they're the spec. Framing at the prompt boundary is the
defense. v3 attachment classifier extends the same discipline
to publicly-lifted issues whose bodies a committer hasn't
curated.

#### Why "LLMs agree" is not "correct"

Executor-reviewer convergence is not correctness. Two LLMs
can confidently approve a subtle logic bug that passes lint
and unit tests. "CI is the primary quality layer" handwaves
race conditions, perf regressions, and data-shape
compatibility.

The mitigation is **procedural rigor + an aggregated decision
record**, not more sophisticated review prompts:

- **Procedural rigor.** Always run code review; always run a
  multi-agent check; always write and run tests. The
  procedure is what keeps the bar from drifting; an LLM
  confidently waving away a scanner warning doesn't pass
  procedure.
- **Reviewer-proposes-failing-test.** When the reviewer
  approves a fix, it must first propose a concrete test that
  would fail without the fix and pass with it. The executor
  iterates until the test exists and matches. Forces the
  correctness claim into executable form.
- **Decision-record aggregation.** Every run emits a
  structured record (prompts used, tier/model/agent chosen,
  files touched, tools called, tests run with results,
  options considered and discarded, final outcome). Over
  time, this substrate lets future agents verify
  completeness, spot missed options, and do post-mortem
  analysis when production bugs trace back to a Hubert-shipped
  PR. v1 ships the records locally (to `hubert-log` repo);
  v2 aggregates into a query surface (Bloveate or similar).

Neither mitigation is sufficient alone. Together they're a
load-bearing defense against the confident-but-wrong failure
mode.

#### Why CI is the primary quality layer

The reviewer agent reads CI results, not just the diff.
Tests passing, lints passing, type checks passing — each
is *deterministic evidence* of correctness that an LLM
doesn't have to generate. This confines the reviewer's
judgment to "is this the right change?", where LLM review
is strong, and away from "does the code even work?", where
deterministic tooling answers better and cheaper.

The pattern:

- **Every PR gets a minimum CI pass before the reviewer
  runs.** Lint, fast unit tests, type check. A red check
  is stop-the-line — the reviewer shouldn't even open the
  diff until cheap gates are green.
- **Expensive stages gate on `ready_for_review`.** Pair
  each with a `concurrency` group keyed on PR number +
  `cancel-in-progress`, so pushing a new commit during
  review kills the in-progress run instead of double-
  billing minutes.
- **Agent-requestable stages use labels, not slash
  commands.** Labels are persistent UI state, trivially
  queryable via API. The reviewer's "I want X checked"
  action becomes an idempotent label write.
- **Reviewer agent reads the CI status check list as
  context.** A green check is not a rubber stamp — the
  reviewer still judges scope fidelity and design — but
  treats the deterministic layer as ground truth for
  "does it work."

Scope discipline that matters: bake the *hooks* for all
of this into v1 (reviewer reads status checks; workflows
use concurrency groups; agents use labels not comments)
but ship only *one real stage* — lint + fast tests. Add
stages only when a real bug slips through the current
set. Agent-requested expensive workflows need a budget
gate, not free rein — enforce a per-PR cap (the
orchestrator refuses to add the label past a threshold,
or the label-triggered workflow checks a budget state
before running).

---

## 4. Trust and security — **[Now]**

### TL;DR

- **Trust gate is at origination, not at merge.** Hubert
  acts iff the issue author has commit access to the
  target repo, or is `hubert-is-a-bot`. Everyone else is
  silently ignored.
- **Hubert is a trust amplifier.** Inherits the trust
  posture of the underlying GitHub repo and adds nothing.
  Defenses against credential theft, branch protection,
  and 2FA enforcement live at the GitHub layer, not in
  Hubert.
- **GitHub is the lock and the kill switch.** All
  coordination, including the global stop signal, is
  through GitHub state — works on stateless deployments
  and Evan can hit the stop switch from his phone.
- **Defenses against runaway costs and runaway recursion
  are baked into the orchestrator and execution prompts**,
  with the orchestrator workflow and the runner binary as
  sanity-check backstops.

### What write access means under Hubert

Adopting Hubert meaningfully changes the trust posture
of "committer" on a watched repo. Before Hubert, a
committer with write access could *propose* changes that
other humans reviewed and merged. Under Hubert, a
committer can drive **arbitrary code execution on the
deployment's Kubernetes cluster against the deployment's
shared LLM billing**, simply by filing issues. The
compromised-committer threat (secondary threat 1 below)
is still out of scope for Hubert's design, but the
blast radius of a compromise is larger than it is on an
ordinary GitHub repo.

Concretely, a legitimately-granted committer (or an
attacker who compromised a committer account) can:

- Dispatch K8s Jobs in the Hubert cluster up to the
  per-issue budget cap, per issue filed, across all
  watched repos the compromised account has access to.
- Consume LLM tokens against the deployment's billing
  (Anthropic, Google, OpenAI, OpenRouter free pools) up
  to the per-day cost cap.
- Cause the runner to clone the target repo, run its
  build/test commands (as configured in
  `.hubert/README.md`), and exfiltrate whatever fits in
  a commit body, PR description, or issue comment.
- Push branches on watched repos (inside the
  `$HUBERT_BRANCH` scope) and open PRs the exec-bot is
  permitted to open.

The defenses Hubert ships are:

- **Budget caps** (per-issue and per-day) bound the
  compute/LLM blast radius. An adversarial committer
  cannot exhaust the shared Anthropic bill in a day.
- **Branch protection and the two-identity split** bound
  the code-reaching-main blast radius. Exec-bot can't
  merge; a human (or review-bot, once it exists) must
  approve.
- **Issue-body-as-data** framing in the execution prompt
  prevents quoted-log-as-instruction style injection.
- **Kill switch** (the `hubert-stop` label) lets the
  operator halt Hubert across all watched repos from
  their phone.

The defenses Hubert deliberately does NOT ship are:

- Anything that would limit what a *trusted* committer
  can file issues about. Restricting issue topics is
  policy, not security, and it's a choice made by the
  repo, not Hubert.
- Independent credential defense. If a committer's
  GitHub account is compromised, Hubert is compromised
  with it. 2FA, hardware keys, branch protection, org-
  level SSO: all GitHub-layer controls. Hubert inherits
  them; Hubert does not replace them.

**Adoption checklist.** Before pointing Hubert at a
repo, the operator should:

1. Audit the collaborator list. Remove ex-collaborators
   who no longer need write. Hubert's trust set is the
   collaborator set; stale collaborators are stale
   trust.
2. Require 2FA (preferably hardware keys) on every
   account in the collaborator set. Org-level
   enforcement if the repo is in an org.
3. Configure branch protection on `main`: required
   status checks, required non-author approval,
   restrict who can push (usually: only the exec-bot
   and humans, not anonymous CI).
4. Set `budget_per_issue_usd` and `budget_per_day_usd`
   in `.hubert/README.md` to values the operator is
   comfortable losing in a single compromise.
5. Decide which path classes are human-required vs
   shadow-mode vs auto-merge (see § 10 on the HITL
   gradient).

Adopting Hubert without running this checklist is
valid, but the operator should understand what they're
trading for convenience.

### Threat model

**Primary threat: a non-committer triggers Hubert work on a
public repository.** The failure mode the trust gate exists
to prevent. The concrete instance is **pidifool**: anyone
in the world can file an issue against a public Go PDF
parser, and we want Hubert to be able to act on legitimate
bug reports without acting on malicious ones. v1 answer:
"Hubert ignores all non-committer issues entirely." v2
answer: the **lift** mechanism lets a committer manually
promote an issue to actionable status.

**Secondary threats:**

1. **Compromised committer account.** If an attacker takes
   over Evan's GitHub account (or any other committer's),
   Hubert will happily act on their malicious issues.
   *Mitigation:* lives at the GitHub auth layer (2FA
   enforcement, hardware keys, branch protection). Hubert
   adds no independent defense. **Document explicitly** so
   users don't assume Hubert is defense-in-depth against
   credential theft.

2. **Malicious attachment in a trusted-author issue.** Evan
   files a perfectly legitimate pidifool bug report and
   attaches the PDF that crashed the parser. The PDF
   itself is the injection vector — either against the
   parser (the bug we want to fix) or against the LLM
   reading the issue (which might extract instructions
   from PDF metadata/text). *Mitigation v1:* execution
   agents that consume attachments treat attachment
   content as data, not instructions; the execution
   prompt makes this explicit. *Mitigation v2:* an
   isolated classifier pass on attachment content before
   any tool-using agent sees it.

3. **Budget-of-spend DoS.** A confused execution agent
   stuck in a loop, running Bash commands forever, or
   pulling a giant transitive dependency tree.
   *Mitigation:* per-issue and per-repo cost caps,
   enforced by both the prompt and the runner binary (the
   runner SIGTERMs the LLM CLI when tracked spend exceeds
   the per-run cap). Kubernetes-side `activeDeadlineSeconds`
   is a second backstop against a runner that fails to
   self-limit. Because cost is reported by the CLI *after*
   a tool call completes, the SIGTERM gate is a soft
   ceiling with up to one tool call of overshoot; a single
   adversarially-expensive tool call can exceed the cap by
   one call's worth. The recovery path (see §3 "Why
   recovery is a first-class flow") handles overshoot by
   pivoting to a cheaper backend on the next dispatch, not
   by retrying at the same size.

4. **Execution-agent tool misuse.** The execution agent
   has Bash, Edit, and Write tools and runs inside a K8s
   Job with capabilities set by the deployment's admission
   policy. A buggy or adversarial prompt could in
   principle have it touch files outside the working
   tree. *Mitigation v1:* each execution runs as its own
   K8s Job in its own pod with an `emptyDir` working
   volume; the pod has no access to any other Job's
   state, no shared PVC, no host mounts. The reference
   deployment's `ValidatingAdmissionPolicy` rejects
   `hostNetwork`, `hostPID`, and privileged containers at
   admission time. The execution prompt also explicitly
   forbids leaving the working tree; the admission policy
   is the structural backstop if the prompt doesn't hold.
   *Mitigation v2:* tighter admission policy (read-only
   root filesystem, egress NetworkPolicy) and per-project
   CLI allowlists.

5. **Recursive plan-decomposition fork bomb.** Hubert can
   file sub-issues against itself, and Hubert acts on its
   own issues (they pass the trust gate). A confused
   orchestrator could file three sub-issues that each
   lead to three more. *Mitigation:* each sub-issue
   carries a `decomposition-depth: N` tag in a structured
   comment; Hubert refuses to act on issues at depth > 3
   without escalation. Orchestrator prompt enforces;
   runner cross-checks.

6. **Hub of trust failure.** `hubert-is-a-bot`'s PAT is itself
   a credential. If it leaks, an attacker can do anything
   Hubert can do. *Mitigation:* PAT scoped to only the
   watched repos, stored in the host's keyring or a K8s
   Secret, rotated on a schedule, with an audit trail of
   recent `hubert-is-a-bot` actions visible in the GitHub UI.
   v2 GitHub App migration replaces this with installation
   tokens that are short-lived and per-repo by
   construction.

**Out of scope for v1:**

- Attachment-content classifier (v2).
- The "lift" mechanism for promoting public-user bug
  reports (v2 or v3).
- Defense against a compromised committer account (lives
  at the GitHub layer).
- Defense against attacks on Anthropic, OpenAI, Google, or
  GitHub themselves.
- Defense against supply-chain attacks on the LLM CLIs,
  Hubert's dependencies, or the deployment's image.
  Standard module hygiene and image pinning apply.
- Defense against a compromised deployment image. The
  operator owns the image per section
  [5](#5-image-contract); if the image is malicious,
  Hubert inherits that. Pin digests, review Dockerfiles.

### Layered defenses around trusted-but-risky action

Even within the trusted set, Hubert applies budget and
sanity limits so a buggy issue or confused agent can't do
unbounded damage:

1. **Per-issue cost cap.** Default $5 (configurable per
   repo). Enforced by the execution prompt and by the
   runner binary (SIGTERM on the LLM CLI subprocess when
   tracked spend exceeds the cap).
2. **Per-day per-repo cost cap.** Default $50
   (configurable). `hubert-snap` sums `🤖 hubert-cost`
   comments into `daily_spend`; the orchestrator prompt
   refuses to emit `dispatch-*` actions past the cap; the
   workflow cross-checks the parsed action list. Resets at
   UTC midnight.
3. **Iteration cap on the executor-reviewer feedback
   loop.** After 3 rounds of "PR rejected, fix it"
   without convergence, Hubert labels the issue
   `hubert-stuck`, posts a summary of what was tried, and
   waits for human intervention.
4. **Stale-lock reaping.** An execution agent that
   crashes, OOMs, or rate-limits leaves its assignment +
   heartbeat comment behind. Next orchestrator tick sees
   the stale heartbeat (>30 min old, no associated open
   PR) and reaps it: posts a `🤖 reaping stale run X`
   comment, unassigns, issue becomes dispatchable again.
   Handles all crash modes uniformly.
5. **Global kill switch via GitHub.** A designated control
   issue (typically `hubert-is-a-bot/hubert-config#1` or a
   per-repo `.hubert-stop`). Open + labeled `STOP` → the
   orchestrator workflow exits at the top before invoking
   any LLM; the runner re-checks on Job startup to catch
   anything queued before the flip. Works across both
   planes; flip from phone.
6. **Ephemeral working trees.** Each execution runs in its
   own fresh `emptyDir`. No carryover, no shared state, no
   cross-run corruption.
7. **`hubert-is-a-bot` PAT scoping.** PAT scoped to only the
   watched repos. Even if leaked, blast radius is bounded
   to repos already exposed to Hubert.
8. **Recursive decomposition depth limit.** Sub-issues
   carry a `decomposition-depth` tag; Hubert refuses
   action at depth > 3 without escalation.
9. **Per-repo and per-issue pause labels.** In addition
   to the global kill switch: a `hubert-paused` label on
   a repo's kill-switch issue pauses Hubert for that repo
   only. The same label on an individual issue pauses
   work on that issue without closing it. Handles the
   common cases (one repo is mid-migration and needs
   manual hands only; one issue turned out more sensitive
   than expected) without the all-or-nothing global stop.
   The orchestrator workflow checks these labels before
   emitting any `dispatch-*` action for the affected
   target.

### Crash and recovery

The concrete failure mode that motivates the recovery
design is **the cycle-4 rate-limit incident** that
preceded Hubert's design: an execution agent in a long
session hit an Anthropic rate limit mid-flight, the
orchestrating session noticed only by accident, and the
user had to manually salvage state from a partially-
written branch.

The Hubert design avoids that failure mode by
construction:

- The execution agent's last action before any large/slow
  operation is to update its heartbeat with what it's
  about to do. Crash mid-operation → heartbeat trail is
  the recovery breadcrumb.
- A stale heartbeat triggers reaping on the next
  orchestrator pass. Reaping is non-destructive: it
  unassigns and posts a summary but does not touch the
  branch or PR.
- When Hubert dispatches a new execution for that issue,
  the new agent reads the thread (including the reaped
  run's heartbeat trail and reaping comment) and decides
  whether to resume from where the previous run left off
  at HEAD of the branch, start over, or escalate.
- The reviewer never trusts an executor's self-report of
  completeness; it independently verifies the PR against
  the issue body.

---

## 5. Image contract — **[Now]**

Defines the interface between Hubert upstream and a
Hubert deployment's execution image. This is what a
deployer must provide for Hubert's execution and reviewer
Jobs to run.

A Hubert execution image MUST provide:

### Binaries on `$PATH`

- `hubert-runner` — the runner binary shipped by upstream.
  The Job's entrypoint invokes this with arguments
  describing the task.
- `git` — for cloning, committing, pushing.
- `gh` — GitHub CLI, used by the runner for lock
  operations (assignment, heartbeat comments, label
  writes, PR creation).
- At least one supported LLM CLI matching whichever
  backends the orchestrator will route to:
  - `claude` — Anthropic Claude Code, non-interactive via
    `claude --print`. Expected present by default.
  - `opencode` — OpenCode, non-interactive via
    `opencode run <prompt> --dangerously-skip-permissions`.
    Optional. Enables OpenAI models and free tiers.
  - `gemini` — Google Gemini, non-interactive via
    `gemini -p <prompt> --yolo`. Optional. Enables
    Google Search.

**CLI version pinning.** The image MUST pin each CLI to a
specific version (or a tested-compatible version range).
Unpinned CLIs rot: session-record schemas, flag syntax, and
output formats change across releases, and Hubert's decision
records and CLI adapters depend on stable shapes. The image
manifest / Dockerfile documents the pinned version; the
runner logs the version at startup for audit. CLI
version-bump PRs against the image are gated on re-running
the end-to-end test suite.

The orchestrator's dispatch actions name a backend
(`agent=claude|opencode|gemini`). The runner invokes that
backend's CLI. If the CLI is missing, the runner exits
non-zero with a clear error; the orchestrator's next pass
sees the failure and either retries with a different
backend or escalates.

### Environment variables

Hubert's Job template injects:

| Variable             | Meaning                                                   |
|----------------------|-----------------------------------------------------------|
| `HUBERT_RUN_ID`      | Unique run id (ULID). Used in comments, branches, logs.   |
| `HUBERT_REPO`        | `owner/name` of the target repo.                          |
| `HUBERT_ISSUE`       | Issue number (set in execution mode).                     |
| `HUBERT_PR`          | PR number (set in reviewer mode and iterate execution).   |
| `HUBERT_MODE`        | `execution` or `reviewer`.                                |
| `HUBERT_ITERATION`   | 0 for fresh executions; ≥1 for iterate.                   |
| `HUBERT_AGENT`       | `claude` / `opencode` / `gemini`.                         |
| `HUBERT_MODEL`       | Model identifier to pass to the CLI.                      |
| `HUBERT_TIER`        | Tier name. Informational; limits are set at admission.    |
| `HUBERT_BUDGET_USD`  | Per-run cost cap.                                         |
| `HUBERT_WORKTREE`    | Directory to clone into (typically an emptyDir mount).    |
| `HUBERT_BRANCH`      | Branch name the runner should create or update.           |

Credentials arrive via a secret mount the deployment
configures:

- `GITHUB_TOKEN` or equivalent — PAT with push/comment
  permission on the target repo. Used by `gh` and `git`.
- Provider-specific credentials for whichever CLIs the
  image ships (`ANTHROPIC_API_KEY` for `claude`,
  OpenCode's credential file under
  `$HOME/.config/opencode/auth.json`, a Google credential
  for `gemini`).

The specific secret layout is deployment-defined; Hubert
asserts only "the CLIs must be able to authenticate when
invoked."

### Writable scratch

The runner expects a writable directory at
`$HUBERT_WORKTREE` (defaults to `/workspace`). The
deployment mounts this as an emptyDir or equivalent
ephemeral volume. It must be:

- Empty at Job start.
- Writable by the Job's user.
- At least 8Gi on standard deployments (the reference
  admission policy enforces this ceiling; smaller is
  fine for lightweight projects).

Anything written here dies with the pod. The runner does
all its work inside this directory; it MUST NOT write
outside it.

**Clone-time responsibilities.** The runner does the clone
itself (not an init container), so the runner handles:

- `git clone --depth 1` by default; full history fetched
  on-demand if the execution prompt needs it.
- Submodules: `git submodule update --init --recursive` if
  the repo uses them. Failure here is an escalation
  (`need-image: submodule-support`), not a silent skip.
- LFS: `git lfs pull` only if `git lfs` is on `$PATH` and
  the repo uses LFS. Missing LFS support on an LFS repo is
  an escalation (`need-image: lfs-support`).
- Project setup: the execution prompt runs whatever the
  per-repo config's `build` command requires (`npm install`,
  `go mod download`, container builds for local tooling).
  The image is responsible for having the toolchains;
  fetching project-local deps is execution work, not image
  scope.

Per-repo config (`.hubert/README.md`) is where operators
document project-specific clone quirks ("this repo needs
submodules", "run `make setup` before first build").

### Network egress

The Job needs egress to:

- `github.com` / `api.github.com` / `ghcr.io` — clone,
  push, API calls.
- `api.anthropic.com` — if shipping `claude`.
- `api.openai.com` / `opencode.ai` / `openrouter.ai` — if
  shipping `opencode`.
- `generativelanguage.googleapis.com` — if shipping
  `gemini`.

No inbound connections required. Cluster-level egress
policy (e.g., NetworkPolicy) is a deployment concern; the
runner doesn't assume anything is blocked.

### User / filesystem posture

- SHOULD run as non-root; the reference admission policy
  rejects privileged containers.
- `/tmp` SHOULD be writable (some CLIs use it for
  intermediate files).
- MAY have a read-only root filesystem, provided
  `$HUBERT_WORKTREE` and `/tmp` are writable mounts.

### Runner invocation

The Job's `command`/`args` invokes:

```
hubert-runner \
    --mode=${HUBERT_MODE} \
    --agent=${HUBERT_AGENT} \
    --model=${HUBERT_MODEL} \
    --repo=${HUBERT_REPO} \
    --issue=${HUBERT_ISSUE} \
    --run-id=${HUBERT_RUN_ID} \
    --budget-usd=${HUBERT_BUDGET_USD} \
    --worktree=${HUBERT_WORKTREE}
```

The runner reads its prompt (execution or reviewer) from
embedded strings compiled into the binary — no prompt-file
mount is required.

### What Hubert upstream does NOT prescribe

- Base image. `alpine`, `debian`, `distroless`, `wolfi` —
  deployer's choice.
- Which CLIs to include. Minimum one; more is fine.
- How credentials are mounted. K8s Secret + `envFrom`,
  IRSA on EKS, Workload Identity on GKE, a sidecar
  fetching from Vault — all fine.
- Image registry, tag strategy, build pipeline.
- Runtime resource limits beyond the admission-policy
  ceilings.

### Failure modes

Contract violations surface as legible Job failures, not
mysterious hangs:

- Missing CLI → runner exits with `agent not found`,
  posts an escalation comment naming the missing binary.
- Missing credential → CLI invocation fails, runner
  captures and posts escalation.
- Worktree not writable → runner fails at clone step,
  posts escalation.
- Admission policy rejects the Job → Job never starts;
  next orchestrator pass sees `kubectl get job` failure
  and treats it as implicit escalation.

---

## 6. v1 implementation plan — **[Now]** and **[Soon]**

v1 is the minimum lovable version Evan can point at a
single private repository (probably **restaurhaunt** or
**speakeasy**) to replace the manual plan-files-in-the-repo
orchestration loop.

### No scope reduction

Any execution agent that picks up this plan: **deliver all
of v1, not the easy parts of v1.** Each task is in scope
unless explicitly moved to "Out of scope." If a task turns
out larger than expected, file a sub-issue with
`decomposition-depth: 1` and link it from the parent — do
not silently drop it.

### Prerequisites (must be resolved before task 1 starts)

These are not v1 tasks — they're gates on starting v1. If
any is missing, starting implementation is wasted work.

- **Container runtime on the dev machine.** Docker *or*
  Podman must build images locally. Hubert's runner ships
  as a container image; without a local build/run loop the
  iteration cost on image-layer changes is prohibitive.
  The dev machine currently lacks a working runtime; this
  is the highest-priority prereq. Either path works —
  Podman is friendlier to Fedora Silverblue / flatpak
  sandboxing, Docker is friendlier if `hermetic`'s
  existing `docker-image.yml` workflow is the reference.
- **`corral` test repo.** A dedicated private GitHub repo
  (`nemequ/corral`, "OK corral") used as v1's throwaway
  testbed — the repo the end-to-end test (§6 task 15)
  drives against. Requirements:
  - `hubert-exec-bot` and `hubert-review-bot` both added
    as collaborators with write access.
  - `main` branch protection: required-approval-from-non-
    author, required status checks (the `hubert-ci.yml`
    workflow).
  - Empty or near-empty initial content; issues come from
    the test harness, not from humans using the repo for
    real work.
- **PATs provisioned.** Three fine-grained PATs per the
  two-identity split (§6 task 11) plus the log sink (§6
  task 13):
  - `hubert-exec-bot`: commits / PRs / comments on watched
    repos.
  - `hubert-review-bot`: review / merge on watched repos.
  - `hubert-log-bot`: write-only to `hubert-log` repo.
  The existing `nemequ-hermi` PAT in
  [`../hermetic/values.local.yaml`](../hermetic/values.local.yaml)
  is the reference for PAT-handling practice; the new
  Hubert PATs live in their own secret set (both as
  GHA secrets on watched repos and as K8s Secrets in
  the `ekdromos` cluster).
- **CLI availability.** `claude`, `opencode`, `gemini`
  all reachable from the dev machine (via flatpak-spawn
  on Silverblue) for the multi-agent implementation
  strategy in Appendix A. Missing CLIs narrow the agent pool
  but don't block — v1 can be built with a subset as
  long as at least two backends are available for
  cross-check.
- **`hubert-log` repo created.** Private repo under
  `hubert-is-a-bot/hubert-log`; the log-bot PAT scopes to it only.
  No content prereq; the runners create the directory
  structure on first write.
- **`hubert-is-a-bot/hubert-config` repo + kill-switch issue
  exist.** The global kill switch referenced throughout
  the design (`hubert-is-a-bot/hubert-config#1`) needs to
  exist *before* the first workflow tick. Bootstrap is:
  - Create the `hubert-config` repo under the
    `hubert-is-a-bot` account (or whichever org-scoped
    account owns the deployment; the repo owner is a
    deployment choice, not a hard requirement).
  - Open issue `#1` with the title "Hubert kill switch"
    and leave it in the open state unlabeled. Labeling
    it `STOP` is what triggers the global kill; an
    unlabeled open issue is the normal state.
  - Grant the exec-bot and review-bot PATs *read-only*
    scope on this repo. The kill switch is a read path
    for Hubert; only a human flips the label.
  - Document the repo URL in the deployment-level
    config (K8s Secret or env var on the runner, so
    `hubert-snap` knows where to look). The repo is
    the source of truth; hard-coding the URL in the
    binary is wrong.
  This is a [Now] prerequisite because the [Now]
  kill-switch implementation (single repo-level label)
  doesn't use it — but the [Soon] three-tier expansion
  does, and starting a deployment without the config
  repo means someone has to go back and bootstrap it
  later. Cheaper to do it now.
- **PAT rotation helper script + expiry monitoring.**
  A small idempotent script (Bash or Go) that, given a
  list of PAT secret names and target scopes, checks
  each PAT's current expiry via the GitHub API and
  warns when any is within 14 days of expiring. Real
  rotation automation (programmatic mint + secret
  redistribution) is [Later] — it needs org-level
  admin scopes Hubert deliberately doesn't have. But
  *monitoring* is cheap and catches the "leaked PAT
  lives until an operator notices" failure mode early.
  Ship the monitoring script as part of the
  deployment prereqs and run it weekly via cron or
  a GHA scheduled workflow against `hubert-log`.

### [Now] first-increment tasks

The [Now] band is deliberately reduced from the original v1
envelope. The goal of *Now* is to prove the loop
end-to-end with a human clicking Merge — not to ship the
polished cross-backend, auto-merging, full-observability
product. Everything [Soon] is earned by the [Now] loop
working.

**[Now] tasks:** 1, 2 (minimal), 3, 4 (minimal), 5, 6,
7 (minimal), 15 (basic loop only).

**Scope-reduction summary (applies throughout [Now]):**

- Single backend. The runner shells out to `claude` directly.
  No `internal/cliadapter/` abstraction yet.
- No cross-repo/cross-tick memory. No `recent_action_hashes`
  in the snapshot; no `provider-health.json` blob.
- Single kill-switch granularity: one repo-level label short-
  circuits the workflow. Three-tier split is [Soon].
- No reviewer auto-merge. The reviewer agent does not exist
  in [Now], or if it does, it posts review comments only
  and never merges. **A human merges.** This is a hard
  [Now] constraint and it's what the HITL gradient (§ 8)
  formalizes later.
- Single-stage CI (lint + fast tests).
- No decision records, no structured-JSON logging layer —
  stderr human-readable logs are fine for *Now*.

#### 1. Project skeleton — **[Now]**

> **[Now] scope:** ship only the `cmd/` and `internal/`
> packages needed for the single-backend minimal loop.
> The following packages are **deferred to [Soon]** and
> should not be created yet: `internal/cliadapter/`
> (single-backend *Now* shells out to `claude` directly),
> `internal/records/` (decision records are [Soon] task
> 13), `internal/idempotency/` ([Soon] task 12). Leaving
> them unscaffolded prevents half-finished abstractions
> leaking into the codebase before their contract is
> earned.

- `go mod init github.com/<owner>/hubert` (final repo path
  TBD).
- Go layout:
  - `cmd/hubert-runner/` — the in-Job runner binary.
  - `cmd/hubert-dispatch/` — the GHA-side Job submitter,
    ported from the reference deployment's `k8s.go`.
  - `cmd/hubert-snap/` — the per-repo snapshot builder.
  - `internal/githubapi/` — shared `go-github` wrappers:
    snapshot types, lock acquire/heartbeat/release, label
    writes, comment parsing.
  - `internal/dispatch/` — Job template +
    admission-policy-compliant spec construction.
  - `internal/orchestrator/` — parse the structured action
    list from orchestrator output; action types as a
    tagged union.
  - `internal/runner/` — clone, CLI invocation, prompt
    dispatch, checkpoint-and-exit plumbing used by the
    runner binary.
  - `internal/cliadapter/` — the **primary portability
    surface**. Unified interface for invoking `claude`,
    `opencode`, and `gemini` CLIs. Normalizes tool-call
    events, cost updates, and stdout into a common event
    stream. Consumed by both the runner and the
    orchestrator workflow step. See task 10.
  - `internal/budget/` — per-run and per-day cost tracking
    helpers.
  - `internal/records/` — structured decision-record
    construction and emission to the centralized sink.
    See task 13.
  - `internal/idempotency/` — action-hash computation and
    dedup lookup against recently-executed action hashes.
    See task 12.
- Embed the prompt files via `//go:embed` so the runner is
  fully self-contained.
- `Makefile` with `build`, `test`, `lint` targets. Build
  produces three static binaries under `bin/`.
- A minimal `.github/workflows/ci.yml` that builds and
  tests Hubert itself (not the tick workflow — that's task
  4).

#### 2. The runner binary (`cmd/hubert-runner`) — **[Now]** (minimal)

> **[Now] scope:** single backend. The runner shells out
> directly to `claude --print --model <model>` rather
> than through the CLI-adapter abstraction. The JSON
> decision-record emission (task 13) is deferred. The
> checkpoint-and-exit recovery markers are parsed but
> only a minimal subset is acted on (`hubert-stuck` +
> plain failure); the full pivot table is [Soon] task 8.

Runs inside each execution or reviewer Job:

- Read task parameters from env per section
  [5](#5-image-contract).
- **Kill-switch and trust-gate re-check** against current
  GitHub state (the orchestrator already filtered, but a
  stale Job queue could fire after a kill-switch flip).
- **Acquire the lock.** Assign `hubert-is-a-bot` to the issue
  (or post an `in-review` heartbeat comment on the PR in
  reviewer flow). If someone else already holds it, log
  and exit 0.
- **Clone fresh** into `$HUBERT_WORKTREE`.
- **Invoke the chosen CLI** against the embedded prompt:
  - `claude --print --model <model>` (default),
  - `opencode run --dangerously-skip-permissions -m <model>`,
  - `gemini -p ... --yolo -m <model>`.
  Stream stdout/stderr to the pod's log.
- **Heartbeat** every 2 minutes: post/edit the
  `🤖 hubert-run <run-id>` comment with timestamp and
  current tool phase.
- **Watch the budget.** Track cost via the CLI's own
  `--format json` output. Near the cap: SIGTERM the CLI,
  commit what exists, post
  `🤖 hubert-escalate reason=budget`, exit 0.
- **On CLI exit:** commit, push
  `hubert/issue-<N>-run-<run-id>`, open a PR (execution)
  or post the review comment + label (reviewer).
- **Checkpoint-and-exit escalation.** If the CLI emits a
  structured marker (`need-tier: large`,
  `need-backend: cheaper`, `need-backend: alternate` in a
  final comment or commit message), or exits non-zero with
  a recognizable reason (OOM, deadline-exceeded, rate-limit
  429, CLI missing), post the corresponding escalation
  comment naming the recovery hint and exit 0. The
  orchestrator applies the recovery-pivot logic (see task
  8); the runner just surfaces the constraint.
- **Decision record emission.** Before exit (on every
  code path — success, checkpoint, escalation, failure),
  emit a structured JSON decision record to the sink
  (see task 13) covering: prompts used, agent/model/tier,
  files touched, tools called with their outcomes, tests
  run with results, options considered and discarded,
  escalation reason if any, cost, final outcome. Emission
  is incremental per phase where possible so OOM doesn't
  lose the whole record.
- **Two-identity awareness.** In reviewer mode, the runner
  verifies the PR was authored by `hubert-exec-bot` (or
  the configured exec identity) before taking any write
  action. An exec-bot-authored PR is in scope; anything
  else is an escalation (`reason: reviewer-scope violation`).

The runner does NOT resume prior-run LLM sessions.

#### 3. The dispatcher binary (`cmd/hubert-dispatch`) — **[Now]**

Ported from the reference deployment's
`docker/tools/hermes-delegate/k8s.go` with label/env-var
renames:

- Render a `batch/v1` Job + `v1` ConfigMap from a
  `text/template` embedded in the binary. Job container
  runs `hubert-runner` with env vars populated.
- Apply via `kubectl apply -f -` (subprocess; no
  client-go).
- Drop the GCS Fuse mount blocks.
- Keep the existing resource-tier map (small/medium/
  large/xlarge) and the 6h admission ceiling.
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

#### 4. The snapshot helper (`cmd/hubert-snap`) — **[Now]** (minimal)

> **[Now] scope:** omit `recent_action_hashes` and the
> provider-health read. [Now] runs with a single backend
> and a coarse kill-switch, so the two features that
> depend on those fields (action dedup in task 12,
> backend pivot in task 8) are themselves deferred.

Called by the orchestrator workflow to build the per-repo
JSON snapshot. Reads:

- All open issues with author, labels, assignees, recent
  ~5 `🤖 hubert-…` comments each.
- All open PRs with author, labels, head/base, mergeable
  state, CI status, recent review comments.
- The current collaborator list.
- Kill-switch state (for [Now]: a single repo-level
  `hubert-stop` label presence-check; the three-tier
  kill switch and per-issue pause labels are [Soon] task 7
  expansion).
- Today's running cost total (`daily_spend`), summed
  from `🤖 hubert-cost` comments.
- **[Soon]** — add `recent_action_hashes` (last ~60
  minutes of `🤖 hubert-action-hash` comments, used by
  task 12 dedup) and `provider_health` (from the
  `hubert-log:/provider-health.json` blob, used by task
  14 backend selection).

Emits a single JSON document on stdout. The orchestrator
workflow pipes it to `claude --print`.

#### 5. GHA workflow templates — **[Now]**

> **[Now] scope:** single-stage CI; orchestrator step
> invokes `claude --print` directly (no adapter-layer
> backend selection, no OpenCode fallback); no
> action-hash dedup step.

In each watched repo's `.github/workflows/`. Ship as
copy-and-paste templates with a documented `inputs:`
contract; generating them from Hubert is out of scope for
v1.

- `hubert-orchestrator.yml` — triggers: `issues`,
  `issue_comment`, `pull_request`, `pull_request_review`,
  `check_run`, `schedule: "*/10 * * * *"`,
  `workflow_dispatch`. Steps: kill-switch check
  (global + per-repo `hubert-paused` label); run
  `hubert-snap`; feed to the orchestrator-selected
  backend via `internal/cliadapter/` (default Claude
  `sonnet`, falls back to OpenCode on sustained
  Anthropic failure — see task 10); parse action list;
  for each action, compute its idempotency hash and
  skip if already observed in `recent_action_hashes`;
  for the rest, act inline (label / comment / noop) or
  shell out to `hubert-dispatch`, and post a
  `🤖 hubert-action-hash` comment recording execution.
  The backstop tick is 10 minutes, not 2 — webhooks
  cover event latency, and 2-minute ticks exhaust free-
  tier GHA minutes on a single repo in about a week.
  Revisit the cadence when multi-repo usage or
  stale-lock reap latency demands it.
- `hubert-ci.yml` — minimum CI pass (lint + fast tests)
  the reviewer treats as ground truth. Single stage; hooks
  laid in for label-gated expensive stages but no stage
  actually ships.

GHA-side binaries (`hubert-snap`, `hubert-dispatch`,
`claude`, `gh`) arrive on the runner's `$PATH` via an
install step at the top of each workflow.

#### 6. Per-repo config (`.hubert/README.md`) — **[Now]**

Loosely-parsed markdown per
[`.hubert/README.md.example`](.hubert/README.md.example):

- Build / test / lint commands.
- `budget_per_issue_usd`, `budget_per_day_usd`.
- `allowed_backends` — privacy-sensitive projects can
  refuse routing to anything except `claude` even when
  the image ships all three CLIs.
- `default_model`.
- `merge_style`, `branch_pattern`.
- Free-form project notes for agent context.

The orchestrator reads this verbatim as part of its
prompt input; no structured parser in v1.

**Reserved for v2.** The `.hubert/agents/` directory and
the `persona` field in dispatch actions are reserved. v1
ignores both if present, so projects can prototype
persona files in parallel with v1 adoption; v2 activates
them. See [v2 persona design](#v2-per-project-agent-personas).

#### 7. Kill switch — **[Now]** (minimal: single repo-level label)

> **[Now] scope:** one granularity only. A `hubert-stop`
> label applied to any issue in the watched repo short-
> circuits the orchestrator workflow at its first step.
> Zero LLM token spend while the label is present. The
> three-tier split (global + per-repo + per-issue) is
> deferred to [Soon] task 7 expansion.

**[Now] implementation.** The orchestrator workflow's
first step is a single `gh issue list --label hubert-stop
--state open --limit 1`. Any result → the workflow exits
with `kill switch engaged`. The runner re-checks on
startup for the same label (a Job queued before the flip
must not fire).

**[Soon] expansion: three-tier kill switch.** Once the
basic loop is proven, expand to:

- **Global.** A designated kill-switch issue (typically
  `hubert-is-a-bot/hubert-config#1`). Open + labeled `STOP` →
  orchestrator workflow exits at the top across every
  watched repo in the deployment.
- **Per-repo.** A `hubert-paused` label on the local
  kill-switch issue pauses Hubert for that repo only.
  Same pattern, narrower scope. Handles "this repo is
  mid-migration, leave it alone" without shutting down
  the others.
- **Per-issue.** A `hubert-paused` label on an individual
  issue pauses work on that issue without closing it.
  The orchestrator refuses to emit any `dispatch-*` action
  for a paused issue; existing runs complete and lock
  cleanly but no new dispatches fire.

The [Soon] version of this task also formalizes the
cross-repo global kill switch via the prerequisite
`hubert-is-a-bot/hubert-config` repo (see Prerequisites
below) — a prereq in [Now] because the expansion
depends on it.

### [Soon] tasks earned by the [Now] loop landing

These are not "nice to have" — they're the work that
makes Hubert *good* rather than *working*. They're [Soon]
rather than [Now] because they only pay for themselves
once the basic loop has surfaced real failure modes on
real repos. Building them speculatively against a theory
of what will go wrong is exactly the trap the tier system
is meant to prevent.

**[Soon] tasks:** 8, 9, 10, 11 (conditional, see below),
12, 13, 14; plus the task-7 expansion, task-15 shadow-
merge graduation, and the reviewer agent with auto-merge
gated by the human-in-the-loop gradient (see § 4.6 in
[open threads](#10-open-design-threads)).

#### 8. Cost tracking + recovery-pivot flow — **[Soon]**

**Cost tracking:**

- Each runner exit posts `🤖 hubert-cost <run-id> <USD>`
  on the issue with the run's total spend.
- `hubert-snap` sums today's `🤖 hubert-cost` comments
  per repo into `daily_spend`.
- Orchestrator prompt is told `daily_spend` and the cap;
  refuses `dispatch-*` actions over.
- The workflow cross-checks the parsed action list —
  defensive backstop.

**Recovery-pivot (the first-class flow, not a failure
mode):** the orchestrator treats a checkpoint-and-exit
escalation the same way regardless of the underlying
trigger — cost overshoot, OOM, deadline, rate-limit, or
a voluntary "too hard for this backend" signal. The
pivot table:

| Escalation reason       | Next dispatch                             |
|-------------------------|-------------------------------------------|
| `need-backend: cheaper` | OpenCode free pool, same or smaller tier  |
| `need-backend: alternate` | A different provider, same tier         |
| `need-tier: larger`     | One tier up, same backend (max xlarge)    |
| OOM / deadline-exceeded | If last was xlarge, pivot to cheaper; else larger tier |
| Rate-limit 429          | Alternate provider, same tier             |
| Cost overshoot (>cap)   | Cheaper backend (OpenCode free), fresh context |

The orchestrator prompt encodes this table. After N cycles
(default 3) of pivoting on the same issue without
convergence, emit `hubert-stuck` and wait for human
intervention.

OpenCode free pools (BigPickle, Nemotron, qwen-coder,
Hermes, Moonshot) are the primary pivot resource — they
absorb retries that would otherwise blow budget or wait
on Anthropic quota, and the "prefer cheaper" principle
means pivots naturally bias toward the cheap end of the
cost curve.

#### 9. Structured logging — **[Soon]**

Both binaries log JSON lines to stderr with `run_id`,
`phase`, `elapsed_ms`, `repo`, `issue` fields. GHA
workflows pipe the orchestrator-pass output through `jq`
for a legible workflow-run summary. Errors include stack
traces; nominal operation is one line per phase
transition. The logs are for humans tailing runs; the
machine-readable substrate for future tuning lives in
the decision records (task 13), not here.

#### 10. CLI abstraction layer (`internal/cliadapter/`) — **[Soon]**

The primary portability surface. A Go interface
implemented once per supported CLI:

```go
type Adapter interface {
    Invoke(ctx context.Context, req Request) (<-chan Event, error)
}

type Request struct {
    Model       string
    Prompt      string
    WorkDir     string // chroot for the CLI
    BudgetUSD   float64
    AllowTools  []string
    Timeout     time.Duration
}

type Event struct {
    Phase     string  // "tool-call", "output", "cost", "done", "error"
    Timestamp time.Time
    ToolCall  *ToolCall
    CostUSD   float64
    Output    string
    Err       error
}
```

Implementations:

- `claude.Adapter` — shells out to `claude --print
  --model ... --output-format stream-json`, parses the
  streamed events.
- `opencode.Adapter` — shells out to `opencode run
  --dangerously-skip-permissions -m ... --format json`.
- `gemini.Adapter` — shells out to `gemini -p ... --yolo
  --format json` (if/when Gemini CLI supports a
  streaming machine-readable format; v1 may fall back
  to parsing final output).

Consumers:

- **Runner** uses the adapter to invoke the execution or
  reviewer prompt. Budget tracking is driven by `Event`s
  with `CostUSD`; the runner SIGTERMs via context cancel
  when `sum(CostUSD) >= BudgetUSD - margin`.
- **Orchestrator workflow step** uses the adapter to
  invoke the orchestrator prompt. The workflow picks a
  backend per tick: primary is Claude `sonnet`; fallback
  to OpenCode if Anthropic returns 429/5xx sustained
  over the last few minutes (feature-flagged via env
  var initially, fully autonomous after v1 bakes in).

Making the adapter the *only* way Hubert invokes a CLI
means adding a fourth backend (a hypothetical fourth
CLI, or actions-runner-controller's hosted version of
one) is an adapter implementation plus a config entry —
no change to the runner, the orchestrator workflow, or
any prompt.

#### 11. Two-bot identity split — **[Soon]** (promote to [Now] if branch protection blocks the loop)

> **Tier rule:** [Now] requires a human to click Merge.
> If the watched repo's branch protection forbids the
> exec-bot from opening PRs on `main` at all without a
> non-author approver already on the branch, task 11 is
> promoted to [Now] so the loop can function. Otherwise
> it stays [Soon] — the human merger *is* the non-author
> approver during [Now], so the two-identity split buys
> nothing until the reviewer agent exists.

Two GitHub accounts, two fine-grained PATs, two K8s
Secrets:

- **`hubert-exec-bot`** — authors commits, pushes
  branches, opens PRs, posts lock/heartbeat/cost
  comments. PAT scoped to `contents:write`,
  `pull-requests:write`, `issues:write` on watched
  repos.
- **`hubert-review-bot`** — reviews PRs, merges. PAT
  scoped to `pull-requests:write`, `issues:write`,
  `contents:write`. Scoped only to act on PRs whose
  author is `hubert-exec-bot` (runner enforces).

Runner changes:

- Execution-mode runner authenticates as exec-bot.
- Reviewer-mode runner authenticates as review-bot.
  Before any write action, the runner verifies the PR's
  author is exec-bot; anything else is an escalation
  with `reason: reviewer-scope violation`.
- The heartbeat/lock comments in execution mode identify
  the acting bot so the orchestrator can distinguish
  exec-bot and review-bot comments in the snapshot.

Deployment updates:

- The K8s Secret grows two tokens (`GITHUB_TOKEN_EXEC`,
  `GITHUB_TOKEN_REVIEW`). The runner picks one based on
  mode.
- GHA secrets grow `HUBERT_EXEC_PAT` and
  `HUBERT_REVIEW_PAT`; the orchestrator workflow uses
  the review-bot only for its reviewer dispatches and
  exec-bot for everything else.
- Branch protection on watched repos gets configured
  with "require approval from non-author + status
  checks" — satisfied automatically because exec-bot
  authors and review-bot approves.

v2 GitHub App migration collapses this back to one
app with role-scoped installation tokens.

#### 12. Action idempotency — **[Soon]**

Defense against webhook + scheduled-tick races where
two orchestrator passes emit the same action.

> **Known race in the comment-based scheme.** Two
> orchestrator workflows firing concurrently can both
> read `recent_action_hashes` from their respective
> snapshots before either posts its hash comment, both
> miss the dup, and both execute. Addressed in the
> label-based alternative below (see § 10.5 for the
> open-thread discussion of the three options).

**Option A — comment-based (original design).**

- Each orchestrator action (`dispatch-*`, `reap-*`,
  `escalate`, inline label/comment writes) is hashed
  by the workflow:
  `sha256(action_type | target_id | canonical_params_json)`.
- Before executing the action, the workflow checks
  `recent_action_hashes` in the snapshot (which the
  snap helper populates from the last ~60 minutes of
  `🤖 hubert-action-hash` comments across the repo).
  If the hash matches, skip.
- After executing, the workflow posts
  `🤖 hubert-action-hash <hex> <timestamp>` on the
  target issue/PR.
- Dedup window: 30 minutes initially. Covers
  webhook/scheduled-tick races comfortably; short enough
  that a legitimate re-emit after real state change
  still lands. Tunable in [Soon] based on observed false
  dedup rate.

**Option B — label-based (race-closed).**

- Action hash is applied as a label on the target
  (`hubert-action-<short-hash>`). Label application
  against a specific issue/PR is close to atomic:
  `POST /repos/.../labels` is serialized per target on
  GitHub's side. Two concurrent workflows both trying
  to apply the label see one succeed; the other gets
  a 422 or a no-op idempotent success, and then the
  `GET /labels` observation in the pre-check catches
  the dup.
- Labels accumulate; a sweep job (or a stale-label
  filter in `hubert-snap`) prunes labels older than the
  dedup window.

**Option C — drop hash-based idempotency entirely.**

- Rely on per-action target locks instead: issue
  assignment catches the parallel-dispatch case,
  PR `hubert-in-review` label catches the
  parallel-reviewer case, and the `reap-stale-lock`
  action's precondition (stale heartbeat) catches its
  own dup. Inline `label`/`comment` writes are cheap
  enough that an occasional parallel dup is acceptable.

The preferred choice is **Option B** if the label
namespace pollution is tolerable (prune policy
manageable) and **Option C** otherwise. Option A is
rejected for [Soon] because the race is real.

The hash is computed from canonical JSON (keys sorted,
no whitespace) so semantically-identical actions
collide and cosmetically-different ones don't.
`hubert-dispatch` is a no-op on dedup — the hash check
happens before the subprocess invocation, not inside
it.

#### 13. Decision records + `hubert-log` sink — **[Soon]**

Every execution/reviewer run emits a structured JSON
decision record to a centralized sink. v1 sink is a
dedicated GitHub repo (`hubert-log`, owned by
`hubert-is-a-bot`); v2 can aggregate into Bloveate or a
purpose-built query surface without a producer change.

**Record contents** (per-run JSON, stable schema v0):

- `run_id`, `repo`, `issue` / `pr`, `mode`, `iteration`.
- `agent`, `model`, `tier`, `persona` (reserved for v2).
- Prompt sources: embedded prompt hash, persona file
  hash if v2-active, `.hubert/README.md` hash.
- Timeline: phases with timestamps, tool calls with
  names and outcomes, tests run with results, files
  touched.
- Decisions: options considered, options discarded,
  reasons.
- Escalations: reason, recovery hint emitted.
- Final outcome: `success` / `checkpoint` /
  `escalation` / `failure`, cost, exit reason.

**Sink mechanics:**

- One file per run, path
  `runs/<repo-owner>-<repo-name>/<issue>/<run-id>.json`.
- Written incrementally: the runner commits partial
  records at each phase transition via `gh api
  PUT /repos/hubert-is-a-bot/hubert-log/contents/...`. OOM
  loses at most the in-flight tool call, not prior
  phases.
- Authentication via a narrowly-scoped PAT with
  write access only to `hubert-log` — stored as a
  separate K8s Secret, separate from exec-bot and
  review-bot tokens.

**Schema stability:** Version the schema (`schema_v0`
for v1). Producers set `schema_version`; consumers
(v2+ aggregators) branch on it. The v1 substrate
commitment is "the schema doesn't change without
bumping the version and shipping a migrator."

Not in v1: a query surface on top of the records.
Ad-hoc `gh api` queries against the log repo are the
v1 analytic layer.

**Failure-mode contract.** The runner emits records via
`gh api PUT` to `hubert-log`. If the sink is unreachable
(network fault, `hubert-log` repo temporarily down,
log-bot PAT expired), emission MUST NOT block the run.
The runner:

1. Attempts the put with a short timeout (5 s).
2. On failure, logs the skip to stderr in a structured
   line (`record-emit-skipped run-id=... reason=...`).
3. Buffers the record in-memory and retries at each
   subsequent phase transition and at runner exit.
4. On runner exit with any records still buffered,
   writes them as a single final-attempt put; if that
   also fails, logs the abandonment and exits 0.

Decision records are valuable but the loop must not
stop because the audit log is down. Missing records
surface in the log repo as gaps in the `run_id`
sequence; the [Later] replay harness (§ 10.X) knows to
skip over them.

#### 14. Provider-health record in `hubert-log` — **[Soon]**

The cross-tick, cross-repo memory piece. Without it, every
orchestrator tick starts with no idea whether Claude is
currently rate-limited, whether today's OpenCode free pool
has burned its daily quota, or whether a Gemini 5xx storm is
underway. With it, the orchestrator's backend choice is
informed rather than speculative.

**Record shape** — a single blob at
`hubert-log:/provider-health.json`:

```json
{
  "updated_at": "2026-04-18T14:22:10Z",
  "claude": {
    "rate_limited_until": null,
    "last_success_at": "2026-04-18T14:21:47Z",
    "last_429_at": null,
    "recent_5xx_count_10min": 0
  },
  "opencode": {
    "rate_limited_until": "2026-04-18T15:00:00Z",
    "free_tier_estimate_remaining": 3100,
    "last_success_at": "2026-04-18T14:19:03Z"
  },
  "gemini": {
    "rate_limited_until": null,
    "last_success_at": "2026-04-18T13:05:12Z"
  }
}
```

**Mechanics.**

- **Writer:** the runner's CLI-adapter layer updates the
  blob opportunistically on every run — successful calls
  stamp `last_success_at`; 429s stamp `rate_limited_until`
  with the `Retry-After` value (or a 10-min default);
  persistent 5xx in a single run bumps
  `recent_5xx_count_10min`. Writes use `gh api PUT` with a
  `If-Match` etag to collapse concurrent updates (last
  writer wins within a tick; stale update lost is fine —
  the next run overwrites).
- **Reader:** the orchestrator workflow reads the blob
  before invoking the orchestrator LLM, passes it into the
  prompt as `provider_health`. The orchestrator prompt
  already has backend-selection language (§ "Choosing
  agent / model / tier"); provider-health turns the static
  `allowed_backends` list into a live-filtered list.
- **Staleness:** a blob older than 30 minutes is ignored
  (treated as "no info"). Fresh success writes refresh it.
- **Scope:** cross-repo by design. Claude rate limits apply
  to the Anthropic account, not the repo; one repo's 429 is
  every repo's problem, and the shared blob makes that
  visible.

**What this earns.**

- When Anthropic 429s, the next orchestrator tick sees
  `rate_limited_until` in the future and routes the next
  dispatch to OpenCode *without* a wasted dispatch-and-fail
  cycle.
- When a per-repo `allowed_backends` includes only
  `[claude]` and Claude is rate-limited, the orchestrator
  emits `noop(reason="primary backend rate-limited, no
  fallback allowed")` and waits — rather than dispatching
  into a known-429 wall.
- No persistent service. The blob lives in a git repo,
  refreshed by the runners themselves. GHA-only control
  plane preserved.

**What this does not earn.**

- Cross-repo budget pooling. Daily cost is still per-repo.
  Budget pooling is a v2 decision (see § 10.9).
- Deferred-work scheduling ("try tomorrow"). That needs a
  real scheduler, which is v2 (see § 8 "Central
  scheduler").

#### 15. End-to-end test (basic loop) — **[Now]**

Before declaring the [Now] loop working against `corral`:

**Basic loop — [Now].**

- Stand up `corral` (see Prerequisites). `hubert-exec-bot`
  added as collaborator with write. Branch protection on
  `main`: required status check (`hubert-ci.yml`). No
  required non-author approval in [Now] — the human
  clicking Merge *is* the approver.
- Deploy the `hubert-orchestrator.yml` and
  `hubert-ci.yml` workflow templates; populate the
  required GHA secrets (exec-bot PAT; review-bot PAT
  omitted since there is no reviewer agent in [Now]).
- Build the reference image from the hermetic Dockerfile
  with `hubert-runner` added; push to
  `ghcr.io/hubert-is-a-bot/hubert-runner`.
- File a trivial issue ("add a hello world function in
  `hello.go`"). Verify the [Now] loop: orchestrator
  dispatches execution → Job opens PR → CI runs → **a
  human clicks Merge.** No reviewer agent runs.
- File a scope-creep-bait issue; verify the executor
  does the asked-for thing only, and the human reviewing
  the PR can reject scope creep themselves. The reviewer
  agent is [Soon], not [Now].

**Recovery — [Now] subset.**

- Simulate kill switch (the [Now] single-label version):
  apply `hubert-stop` to any issue, confirm the workflow
  exits at step 1 with zero LLM calls.
- Simulate failure: `kubectl delete job/<running-run>`
  mid-flight. Verify the next orchestrator pass reaps
  the stale heartbeat and re-queues (not retries-same).
- Simulate cost overshoot by setting `budget_per_issue`
  unrealistically low. Verify the runner SIGTERMs the
  CLI, commits what exists, and escalates cleanly. The
  recovery-pivot to a cheaper backend is [Soon] task 8.

The [Now] end-to-end test is successful when the human
operator files an issue, walks away, comes back to a
green PR, clicks Merge, and the issue closes
automatically. That's the loop. Everything else is
[Soon].

#### 15a. Shadow-merge-gate graduation — **[Soon]** (replaces old 2-week box)

The reviewer agent with auto-merge is gated behind a
**volume-and-diversity** criterion rather than a time
box. The original 2-week-on-corral graduation plan is
**explicitly rejected** as under-powered: two weeks of
near-empty seed content tells you nothing about the tail
risk of auto-merge on real codebases, and "no false
approvals" is unmeasurable when nothing substantive is
being approved.

**New graduation criterion.** Auto-merge goes live for a
given path class only after:

- **N ≥ 25 shadow-mode merges** against that path class
  (shadow-mode = reviewer approves, human merges),
  spread across...
- **M ≥ 3 distinct repos** (the path class must have
  been exercised in more than one codebase, not just
  `corral`), touching...
- **P ≥ 5 distinct code areas** within the path class
  (e.g., for "backend Go code": handler, middleware,
  storage, serializer, and one other — not 25 merges
  all in `handlers/`), with...
- **Zero human-identified false approvals** — no
  shadow-mode merge that a human would have rejected
  and no post-merge regression traced back to one.

The specific N/M/P numbers are a starting point, not a
contract. Raise them if shadow mode surfaces a near-miss;
lower them only if the post-merge observation window
(§ 10.15) accumulates enough positive
signal to justify it.

**Path-class scope.** Graduation is per-**path class**,
not per-repo and not per-project. A class is a set of
path globs declared in `.hubert/README.md` under the
HITL-gradient block (see § 10.16). Example
classes: `backend-go`, `frontend-react`, `schema`,
`auth`, `ci-config`. "Backend Go code" might graduate to
auto-merge while "CI config" stays human-required forever.

**Regression policy.** Any post-merge regression traced
to a Hubert auto-merge — even if the regression is
surfaced weeks later — reverts that path class to shadow
mode until root-cause is identified and the reviewer
prompt (or class scope) is updated.

The graduation criterion is deliberately stricter than
the original. v1 is building the substrate to make
auto-merge safe; graduation is the moment that substrate
stops being theoretical.

### Verification

Split by tier so the operator knows which criteria
gate [Now] vs [Soon].

**[Now] — loop works against `corral` with a human
merger.**

- `make build && make test && make lint` all pass on
  Hubert itself.
- `hubert-snap` dry-run against `corral` prints a sane
  snapshot JSON (minimal [Now] fields; no
  `recent_action_hashes`, no `provider_health`).
- Orchestrator pass on that snapshot prints a reasonable
  action list against a single-backend prompt (no
  dispatches fire during dry-run; use `-dry-run` on
  `hubert-dispatch`).
- End-to-end [Now] test succeeds against `corral`: file
  an issue, observe PR, human clicks Merge, issue
  closes.
- [Now] kill switch demonstrably zero-cost (single
  `hubert-stop` label present → workflow exits).
- Stale-lock reap recovers from simulated crash without
  human intervention.

**[Soon] — loop works unattended with reviewer
auto-merge gated by the HITL gradient.**

- `hubert-snap` includes `recent_action_hashes` and
  reads `provider_health`.
- Orchestrator backend swap works: force a Claude 429
  in the adapter layer, verify OpenCode fallback on the
  next tick.
- Three-tier kill switch works (global + per-repo +
  per-issue).
- Decision records land in `hubert-log` with every
  phase of a representative run captured; the
  sink-unreachable failure-mode contract is honored
  (log skip, buffer, retry, continue).
- Action idempotency deduplicates a webhook+schedule
  race in the test harness — using the label-based
  (race-closed) implementation, not the original
  comment-based scheme.
- Shadow-merge gate satisfies the volume+diversity
  criterion (§ 15a) for at least one path class before
  that class's auto-merge flips live.
- HITL gradient is parsed and applied — a glob matching
  `human_required` refuses to dispatch; a glob matching
  `auto_merge` permits the reviewer to merge.
- Prompt-portability CI passes for every configured
  backend on the fixture-repo seed issues (§ 10.10).

### Out of scope for v1

See the **canonical deferrals index in § 11** for the
full list, organized by tier. This subsection used to
duplicate that list; it now just points at the
authoritative location to keep the two from drifting.

---

## 7. Reference deployment: hermetic — **[Now]** (operator context)

The `../hermetic` repo is Evan's reference deployment.
Most of the Kubernetes-side infrastructure Hubert needs is
already built, deployed, and running on the GKE cluster
`ekdromos` (GCP project `forklore-491503`). This section
is the port inventory.

### What ports cleanly

**Job submission logic (`docker/tools/hermes-delegate/k8s.go`).**
A ~400-line file that:

- Renders a `batch/v1` Job + `v1` ConfigMap via Go
  `text/template` from a struct. The template is embedded
  in the binary.
- Submits via `kubectl apply -f -` over stdin.
- Waits for the pod to exit Pending, streams logs with
  `kubectl logs -f --all-containers=true job/<name>`.
- Installs SIGINT/SIGTERM handler for best-effort cascade
  delete on exit.
- Supports `-detach` mode.

This is essentially Hubert's `cmd/hubert-dispatch` in
everything but names. Porting means: rename
`hermes-agent/delegated` → `hubert/execution`; replace the
env-var contract (`HERMETIC_IMAGE` →
`HUBERT_IMAGE`, etc.); drop the GCS Fuse mount block.

**Resource tiers** (from `k8s.go`):

| Tier   | CPU req/lim | Memory req/lim | Deadline |
|--------|-------------|----------------|----------|
| small  | 500m / 1    | 1Gi / 2Gi      | 1h       |
| medium | 1 / 2       | 4Gi / 8Gi      | 2h       |
| large  | 2 / 4       | 8Gi / 16Gi     | 4h       |
| xlarge | 4 / 8       | 16Gi / 32Gi    | 6h       |

xlarge fits a full node's worth of resources on the
cluster's `ek-standard-16`. Reuse until a concrete reason
to change appears.

**Admission controls.**
`charts/hermes-agent/templates/delegate-admission-policy.yaml`
is a `ValidatingAdmissionPolicy` + Binding pair. CEL
validations reject any Job created by the agent SA that
doesn't match the delegate template:

- Must carry the delegated-label (rename to `hubert/execution`).
- Must run under the agent SA.
- Image must start with the configured image-prefix.
- Memory limit set and ≤ `maxMemoryBytes`.
- `activeDeadlineSeconds` set and ≤ `maxActiveDeadlineSeconds`.
- `backoffLimit` ≤ 1.
- No `hostNetwork`, `hostPID`, or privileged containers.

`validationActions: [Deny]` — rejected at admission, not
just audited. Scoped to a single namespace via
`namespaceSelector`. 90% of the Hubert admission policy
is a rename pass on this file. Keep ceilings configurable
in values.yaml; defaults (32Gi, 86400s) are reasonable.

**RBAC** (`charts/hermes-agent/templates/rbac.yaml`).
Namespaced `Role` + `RoleBinding`:

- `create, get, list, watch, delete, patch` on `jobs.batch`
- `create, get, list, delete, patch` on `configmaps`
- `get, list, watch` on `pods`
- `get` on `pods/log`

Intentionally **not** a `ClusterRole`. Each deployment is
its own blast radius. The Hubert equivalent preserves this
pattern.

**Workload Identity setup** (`scripts/setup-gcs-fsq.sh`).
Bash, idempotent. Creates GCP SAs, Workload Identity
bindings between K8s SAs and GCP SAs, writes identifiers
to GitHub Actions repo secrets. The `bind_wi` and
`ensure_sa` functions are clean and small; pattern ports
well. The HMAC fallback is only needed if Hubert touches
services without Workload Identity support (it doesn't).

### What does NOT port

**hermes-delegate agent layer** (`agent.go`, `prompt.go`,
`sandbox.go`). hermetic-specific glue for invoking
`claude -p` / `gemini -p` / `opencode run` as child
processes with env scrubbing and output collection.
Hubert's runner has a different shape — it invokes the
CLI inside the Job and interacts with GitHub directly;
there's no "wrap the CLI and collect stdout" layer.

**GCS Fuse context volumes.** hermetic mounts three
subpaths of a single GCS bucket (`…/claude`, `/gemini`,
`/opencode`) so session state, API credentials cache, and
tool config can be shared across invocations. Hubert's
execution agents are fresh runs with no cross-run state;
cloning fresh and pushing a branch is the only
persistence. **Skip the GCS Fuse layer entirely.** It
adds a sidecar container, a CSI driver dependency, mount
latency, and a CEL complication to the admission policy,
and buys Hubert nothing.

**Hermetic's secret shape.** Hermes's large K8s Secret
with Discord tokens, multiple LLM provider keys, Honcho
config, OAuth tokens, etc., is mounted via `envFrom`.
Hubert's secret is much smaller — one Anthropic API key,
one GitHub PAT, plus whatever CLI auth the image needs.

### Decisions worth not relitigating

Things hermetic decided and wrote code against. Keep
unless Hubert has a specific reason to differ.

- **`emptyDir` per Job, not shared PVC.** Each Job's
  `/opt/data` (or equivalent) is a fresh `emptyDir` with
  an 8Gi ceiling, isolated from every other Job. RWO-PVC
  sharing doesn't work (one pod at a time); GCS Fuse for
  source code is genuinely bad (`git status` takes
  seconds); Filestore (RWX NFS) costs ~$50/mo minimum.
  The correct answer is "each Job clones fresh and
  pushes a branch."
- **Prompt delivery via per-Job ConfigMap with
  `binaryData`.** Avoids env-var and arg-list size limits.
  For Hubert the runner reads prompts from embedded
  strings so this is less load-bearing, but the
  ConfigMap pattern is worth keeping in mind for when
  you want to ship a large orchestrator snapshot into an
  executor Job.
- **kubectl shell-out beats client-go at this size.**
  5.2MB vs ~30MB static binary. The only K8s operations
  the binary needs are `apply -f -`, `get pods`,
  `logs -f`, and `delete`.
- **`ValidatingAdmissionPolicy` over Kyverno.** VAP is
  built into K8s 1.30+, CEL expressions are sufficient,
  zero extra install.
- **Container name matters for log querying.**
  `kubectl logs --all-containers=true` merges sidecar
  output with the main container; name containers
  intentionally (`execution`, `reviewer`).

### Current cluster state

For reference, the state of `ekdromos`:

- GKE 1.34, `us-central1`, 2 nodes of `ek-standard-16`.
- GCS Fuse CSI driver enabled (Hubert won't use it).
- Workload Identity enabled, pool
  `forklore-491503.svc.id.goog`.
- Namespace `hermes` exists with the hermes-agent
  deployment running.
- GCP SAs: `hermes-agent`, `hermes-fsq-sync`,
  `hermes-fsq-reader`, `hermetic-gha-deploy`.
- Context bucket: `gs://forklore-hermes-context` (not
  Hubert's concern).

Whether Hubert shares the cluster or gets its own
namespace (`hubert`?) is a v1 decision. Sharing is
cheaper and namespaced RBAC already isolates workloads;
separate clusters would only matter for blast-radius
isolation and nothing about the threat model justifies
the cost.

### Open threads from hermetic

- **Empty stdout from opencode under
  `claude-delegate -k8s`.** The end-to-end smoke test on
  2026-04-18 submitted a Job that completed exit 0 and
  produced no logs from its main container. Unclear
  whether opencode's default output goes to a log file
  rather than stdout, or the `-f prompt-file` path
  silently no-ops on short input, or something else. If
  Hubert uses opencode as an execution backend, this
  needs debugging before exit 0 is trustworthy as a
  success signal.
- **Default `--dangerously-skip-permissions` or
  equivalent.** All three CLIs need a "don't prompt for
  tool approval" flag when run non-interactively.
  `claude --print` is the intended non-interactive mode
  and doesn't need the flag. For write-mode tools (Bash,
  Edit, Write), configure tool allowlist via
  `settings.json` or `--allowed-tools`.
- **GH Actions deploy path.** hermetic's
  `docker-image.yml` builds on push to main, pushes to
  `ghcr.io/nemequ/hermetic`, runs `helm upgrade --install`
  against the cluster using a GHA SA key. Hubert will
  want the same shape.

---

## 8. v2 / v3 roadmap — **[Later]** and **[Someday]**

This section is load-bearing: everything designed but
deliberately left out of v1 lives here so we don't lose it.
If a v1 implementation finds itself reaching for any of this,
revisit the punt rather than silently adopting the deferred
design.

### v2: per-project agent personas — **[Later]**

**The inspiration.** `agency-agents`
(msitarzewski/agency-agents on GitHub) is a library of 144+
agent personas organized into 12 divisions:
frontend-developer, database-optimizer, security-auditor,
schema-migrator, brand-writer, and so on. Each persona is a
markdown file with identity, workflow, deliverables, and
success criteria — no execution substrate, just carefully
crafted role prompts activated conversationally.
`agency-agents` is 100% prompts with 0% coordination; Hubert
is the opposite shape. The marriage is obvious: **Hubert
provides the dispatch / locking / heartbeat / budget
substrate `agency-agents` lacks; projects declare their own
specialists in `.hubert/agents/`.**

**The axis addition.** v1 dispatch actions name `agent` (CLI
backend), `model` (specific model id), and `tier` (K8s
resource envelope). v2 adds a fourth orthogonal field:

- `persona` — a string naming a project-defined specialist,
  resolved against `.hubert/agents/<persona>.md` in the
  target repo.

Example v2 action:

```json
{
  "action": "dispatch-execution",
  "issue": 42,
  "mode": "fresh",
  "agent": "claude",
  "model": "opus",
  "tier": "medium",
  "persona": "schema-migrator"
}
```

The runner, after cloning, reads the named persona file from
the worktree and prepends its contents to the embedded
execution prompt as role context. The embedded prompt still
defines the **mechanics** of the Hubert protocol (lock
acquisition, heartbeat, checkpoint-and-exit, PR opening,
scope discipline); the persona file defines **voice, domain
expertise, and project-specific constraints.** Upstream owns
the protocol; projects own the personas.

**File shape.** `.hubert/agents/<persona>.md`, one file per
persona. Loose markdown with conventional headings but no
strict schema — same "prose, not YAML" policy as
`.hubert/README.md`. Example:

```markdown
# Schema Migrator

## Role
You specialize in Postgres schema evolution for this
service. You write reversible migrations and refuse to
ship changes that can't be rolled back.

## Constraints
- Every migration ships with `up` and `down`.
- Never drop a column in the same migration that adds its
  replacement; split across two deploys.
- Follow conventions under `db/migrations/`.

## Success criteria
- Applies cleanly against a fresh DB and current prod
  schema.
- Rollback tested, not just written.
```

No frontmatter, no YAML, no version field. If a second
structured knob shows up later, graduate at that point.

**Orchestrator policy.** The v2 orchestrator gets persona
selection as an explicit concern:

- If the issue carries a label the project maps to a
  persona (mapping declared in `.hubert/README.md`), use
  that persona.
- Otherwise the orchestrator picks based on issue body
  content and the persona roster in `.hubert/agents/`.
- If no persona fits or the project has no personas
  defined, fall back to the generic execution prompt (the
  v1 behavior, which becomes the `persona=none` case).
  Projects can adopt personas incrementally; a project
  with zero `.hubert/agents/*.md` files behaves like v1.

**Team assembly (v2 aspirational, more likely v3).**
`agency-agents` documents multi-persona workflows — five or
six specialists applied in sequence — but has no mechanism
beyond "the human pastes each one into Claude in order."
Hubert already has the mechanism: sequential Jobs with
GitHub state as handoff. The v2/v3 orchestrator can emit a
**sequence** of dispatches for a complex issue, each with a
different persona:

- `backend-architect` opens a draft PR with design notes.
- `frontend-developer` reads the draft, adds client code.
- `database-optimizer` adds migrations, updates schema.
- `security-auditor` reviews the complete PR.

Each persona is its own Job/pod with fresh context — no
shared conversation, every handoff durable in the comment
trail. The v1 executor-then-reviewer flow is the degenerate
case of this pattern with two fixed personas. Scaling to
arbitrary sequences is an orchestrator-prompt +
action-schema change, not a substrate change. **The v1
substrate does not block team assembly**; that's what we
mean when we say the v1 substrate stays v2-ready.

**Why this is v2, not v1.**

- **v1 is about proving the two-plane substrate works.** A
  persona system is a content-layer feature; shipping it
  before the substrate is battle-tested means debugging
  two things at once.
- **Persona design wants feedback from real use.** The
  right roster is a discovery problem, not a design
  problem. Running v1 across several projects will surface
  which specializations pay off. Designing personas in the
  abstract would overfit to speculation.
- **Bolting on is cheap.** Runner change: ~20 lines
  (resolve persona arg against a worktree path; prepend
  file content to embedded prompt). Orchestrator-prompt
  change: new decision branch. Schema change: one field.
  None of it forces a v1 redesign.

**v1 reservations to make v2 painless.**

- The `.hubert/agents/` directory path is reserved. v1
  ignores it if present; v2 reads it. Projects can start
  prototyping persona files now knowing Hubert will pick
  them up later.
- The `persona` field name in the action schema is
  reserved. v1 orchestrator never emits it; v1 runner
  ignores it if present.
- The label → persona mapping block in `.hubert/README.md`
  is reserved; v1 parses the README loosely and simply
  ignores unknown sections.

### v2: remaining items — **[Later]**

- **GitHub App** migration: replaces PAT, per-repo
  installation, fine-grained permissions, short-lived
  tokens, fixes attribution.
- **Per-org and per-team auto-approve rules.**
- **Cross-repo cost aggregation and global caps.**
- **Dedicated `hubert-log` audit repo** or GitHub
  Discussion for centralized audit trail.
- **Label-driven policy engine for backend / model / tier
  selection.** Taxonomy suggestion:
  `complexity:trivial|moderate|heavy`,
  `area:frontend|backend|infra`,
  `needs:web-search|reasoning|privacy`. v1 lets the
  orchestrator prompt make these decisions informally;
  v2 turns them into a testable routing table.
  **Composes naturally with personas**: `area:*` labels
  drive persona selection; `complexity:*` labels drive
  model/tier selection.
- **Research-phase flow.** A `mode=research` dispatch
  triggered on fresh trusted issues or on
  `@hubert-is-a-bot research` comments. OpenCode with a cheap
  model (BigPickle / Nemotron / codex-mini) does prompt
  triage: what is this issue asking, how large is it,
  what unknowns exist? Result posts as a structured
  `🤖 hubert-research` comment. A `needs: web-research`
  label (or `@hubert-is-a-bot research --web`) escalates to a
  Gemini dispatch for external fact-finding. Feeds into
  execution dispatch with a pre-analyzed problem and an
  informed "prefer cheaper" model choice. v1 ships with
  the orchestrator making informal triage decisions
  inside the orchestrator prompt; v2 formalizes.
- **Expensive CI stages** gated by labels + per-PR
  budget cap. Hooks are in v1; stages aren't.
- **Tighter admission policy:** read-only root
  filesystem, egress NetworkPolicy.
- **Aggregated decision-record query surface.** v1 ships
  per-run records to `hubert-log` (task 13). v2 adds a
  query layer: candidates are **Bloveate** (Evan's
  sibling project — OpenTelemetry-collector pattern fits
  naturally, prompts/outputs configurable as fields) or
  a purpose-built service on top of `hubert-log`.
  Composes with personas: "which persona handled
  complexity:heavy issues best last quarter?" becomes a
  real query.
- **Multi-agent review negotiation with codified
  suppression.** When a security scanner or linter
  produces a finding the executor believes is a false
  positive, v2 lets the executor dispute it — but
  **only via a codified artifact**:
  - a test case demonstrating the false positive, OR
  - a commit to a tool-consumed `suppressions.yml`.
  Chat-history rationalization ("the scanner is wrong
  because …") is not a valid resolution path; the LLM's
  argument to the reviewer is not a substitute for
  evidence the tool itself accepts. The budget gate
  applies: the orchestrator refuses to add an
  expensive-scanner label past a per-PR threshold, and
  the label-triggered workflow cross-checks budget
  before running.
- **Comment-edit webhook semantics.**
  `issue_comment.edited` fires fine; the question is
  what to do with it. v2 answer: edits re-queue an
  issue/PR *only if* `hubert-changes-requested` or
  `hubert-stuck` is set; otherwise the edit is captured
  in the snapshot but doesn't preempt an in-flight run.
  Prevents a typo fix from retriggering a $5 pass.
- **Agent-requestable CI workflows with budget gates.**
  Labels like `ci:integration`, `ci:perf`, `ci:security`
  trigger expensive stages. The orchestrator can request
  them by writing the label, but with a per-PR budget
  cap (the orchestrator refuses to add a new expensive
  label past a threshold, and the label-triggered
  workflow rechecks budget state before running).
  Prevents the agent from spending its way out of a
  scope decision via aggressive label-writing.
- **Honcho memory, per-role.** Memory integration is
  paused for v1, but when it lands the shape matters:
  - *Orchestrator* — safe to read routing-outcome
    memory, project conventions, user-style preferences.
  - *Executor* — safe to read repo conventions, style
    guide, long-lived constraints.
  - *Reviewer* — safe to read repo conventions and
    style guide; **dangerous** to read anything about
    the current PR's own history. Reading executor
    reasoning would defeat the fresh-context separation
    that makes the reviewer a real second opinion.
  The split makes Honcho prompt-integration (orchestrator
  queries Honcho to assemble context) cleaner than
  wiring Honcho in as a first-class architectural
  component.

### v2: central scheduler with reaction-based signaling — **[Later]**

**The motivation.** v1 decides what to dispatch per
orchestrator tick, per repo, with a tiny window of
cross-run state (recent action hashes, provider-health
blob — see § 6 task 14). That's fine when the rate limits,
cost caps, and free-tier budgets fit comfortably. It stops
being fine when they don't: at multi-repo scale with
free-tier-dominated spending, the decisions that matter
most are *cross-repo* and *temporal* — "which repo's work
should we fund with today's remaining OpenCode quota,"
"should we defer this dispatch 6 hours until the Claude
rate-limit window resets," "is there bandwidth for the
xlarge refactor or should it wait until tomorrow." No
amount of per-tick snapshot cleverness gets those right.

**The shape.** A long-running scheduler service (K8s
Deployment, not Job) that owns three decisions the v1
orchestrator can't really own:

1. **Admission.** A dispatch is *emitted* by the
   orchestrator but *admitted* by the scheduler. Between
   the two, the scheduler can defer, model-swap, or
   reject-with-reason based on live provider health, live
   budget state across all repos, and live queue depth.
2. **Model selection under pressure.** When the primary
   backend is rate-limited or the pool budget is tight,
   the scheduler rewrites the action's `model` /
   `agent` / `tier` before admitting it. The orchestrator
   expresses intent ("do this issue"); the scheduler
   decides "how" given current constraints.
3. **Deferral.** Not all work is urgent. The scheduler
   can mark a dispatch as "admit after UTC midnight" when
   the daily cap is exhausted, or "admit when Claude
   429-window clears" when the orchestrator wanted Claude
   but can't have it right now. v1 has no way to express
   "this, but later"; the scheduler adds it.

**Reaction-based signaling primitive.** The coordination
between scheduler and workers is a single GitHub reaction,
not a side channel. Specifically:

- The orchestrator emits a dispatch action. The workflow
  posts a structured `🤖 hubert-pending <run-id>` comment
  on the target issue containing the dispatch spec
  (agent, model, tier, persona) as JSON.
- The scheduler, watching via GitHub webhooks, reacts to
  the pending comment:
  - `:rocket:` — admitted as-is; worker proceeds.
  - `:arrows_counterclockwise:` — admitted with
    rewrites; scheduler posts a reply comment with the
    new spec, then `:rocket:` reacts on its own reply.
  - `:hourglass:` — deferred; scheduler posts a reply
    comment with the `admit_after` timestamp.
  - `:no_entry_sign:` — rejected (persistent
    over-budget, misrouted persona, trust-gate
    failure); scheduler posts a reply comment with the
    reason.
- The worker is dispatched only once a `:rocket:` is
  present. If the scheduler is down or slow, a timeout in
  the orchestrator workflow falls back to v1 behavior
  (immediate dispatch) so a scheduler outage degrades to
  "works like v1" rather than "stops working."

Reactions as the primitive is the key design choice.
They're free, persistent, visible, API-queryable, and
only the bot account can post them — the trust model is
trivial. A comment edit could serve the same role, but
reactions are lighter and don't generate webhook churn
on `issue_comment.edited`.

**The scheduler is an active dispatcher, not just a
gate.** When a deferral resolves — the Claude 429-window
clears, UTC midnight rolls over and the daily cap resets,
a free-tier pool refills — the scheduler does not wait
for the next orchestrator tick to pick the work back up.
It calls GitHub directly: `POST /repos/{owner}/{repo}/actions/workflows/{id}/dispatches`
to spawn the execution workflow with the (possibly
rewritten) dispatch spec, then reacts `:rocket:` on the
original `🤖 hubert-pending` comment so the audit trail
in the issue matches the dispatched run. This is the
push side of the scheduler; the reaction primitive is
the pull side that workers observe. Both exist because
"work is admitted" and "work is started" are different
events — the orchestrator may not run again for minutes,
and deferred work needs to resume within seconds of its
constraint lifting, not at the next scheduled tick.

**Why v2, not v1.**

- Adds a long-running service to a design deliberately
  built without one. v1 has to prove the basic loop
  before adding infrastructure.
- v1's provider-health blob (§ 6 task 14) earns ~80% of
  the cross-repo awareness value at ~5% of the
  complexity. Ship that first; measure whether the
  remaining 20% is worth a scheduler.
- Deferral-as-a-feature is most valuable at multi-repo
  scale with free-tier economics. Single-repo v1 users
  rarely hit the deferral case.
- The reaction primitive is cheap to add later — the
  orchestrator's `🤖 hubert-pending` comment and
  worker-waits-for-rocket-reaction loop can be
  introduced in v2 without breaking v1's
  immediate-dispatch path (the fallback *is* the v1
  path).

**What falls out of scheduler adoption (v2/v3 items that
compose naturally).**

- *Cross-repo budget pooling* (§ 10.9 open thread).
  Scheduler is the natural place to enforce pool-level
  caps rather than per-repo caps.
- *Admission queue persistence.* If the scheduler crashes
  mid-admission, the pending comments in GitHub are the
  source of truth for queue state — the scheduler
  reconstructs its state from open `🤖 hubert-pending`
  comments at startup.
- *Human override.* A human reacting `:rocket:` on a
  deferred pending comment force-admits the dispatch —
  "I need this now" is a real-world need.
- *Per-persona scheduling.* Once personas land
  (§ 8 "per-project agent personas"), the scheduler can
  queue-by-persona — e.g., serialize all
  `schema-migrator` dispatches across repos to avoid two
  migrations landing in the same window.

### v3: the open-source bot use case — **[Someday]**

Makes Hubert usable on public repos with untrusted
contributors (the pidifool motivating case).

- **Attachment-content classifier** as an isolated
  preprocessing pass before any tool-using agent sees
  issue attachments (the PDF-metadata-as-injection
  concern).
- **The "lift" mechanism:** a committer comments
  `@hubert-is-a-bot lift` on an untrusted-author issue to
  promote it into Hubert's actionable set.
- **Per-contributor reputation tracking** for lifted
  issues so Hubert can auto-promote repeat good-faith
  contributors.
- **Default-deny-after-N-hours** for stale lifted issues
  so unactioned lifts don't accumulate.
- **Persona marketplaces / sharing.** If the persona
  ecosystem takes hold, projects may want to import
  `agency-agents` personas directly or share personas
  across related repos. Non-trivial: imported personas
  need to trust the importing project's build/test/lint
  commands, which is a trust escalation worth
  thinking about.
- **Per-role GitHub Apps.** v1 uses two PAT-backed bot
  accounts (`hubert-exec-bot` + `hubert-review-bot`)
  because it's the cheapest thing that gives branch-
  protection-friendly distinct identities. v3 replaces
  them with *one GitHub App per role*: a `hubert-coder`
  App, a `hubert-reviewer` App, and a `hubert-researcher`
  App (and per-persona Apps as the persona system matures
  — `hubert-schema-migrator`, `hubert-security-auditor`,
  etc.). Each App is installed independently per repo
  with its own permission scope, its own rate-limit
  quota, and its own short-lived installation tokens.
  Benefits:
  - **Permission scoping enforced by GitHub**, not by
    Hubert's runner. A coder App without
    `pull-requests:merge` cannot merge even if the
    runner has a bug.
  - **Per-role rate-limit headroom.** Each App is its
    own GitHub principal with its own REST/GraphQL
    budget; a coder App burning through issue-reads
    doesn't starve the reviewer App.
  - **Cleaner open-source story.** Downstream operators
    install "Hubert Coder" from the GitHub Marketplace
    (or a self-hosted equivalent) rather than
    provisioning bot PATs manually.
  - **Cross-App communication** is through GitHub's
    normal event surface — the coder App opens a PR,
    the reviewer App's webhook fires, the researcher
    App can be invoked by either. Aligns with the v2
    scheduler (§ 8) where reactions are the inter-role
    signaling primitive.
  Migration shape: v2 introduces a single
  `hubert-platform` App with multiple installation
  configurations (exec, review). v3 splits it into
  role-specific Apps as the persona/research/review
  roles differentiate enough to justify separate
  identities. v1's two-PAT scheme is the stepping stone.

---

## 9. Installing Hubert (planned) — **[Soon]**

Once implementation lands, an operator will:

1. Clone this repo, `make build`. Install
   `hubert-runner` into a downstream container image that
   also contains `git`, `gh`, and whichever LLM CLIs the
   deployment supports. See section
   [5](#5-image-contract) for the contract.
2. Install `hubert-dispatch` and `hubert-snap` onto the
   cluster's GHA runner (typically via a `setup-` action
   at the top of each workflow).
3. Create a dedicated GitHub account named `hubert-is-a-bot`.
   Generate a fine-grained PAT scoped to the repositories
   you want Hubert to watch. Store it as a GHA secret and
   a K8s Secret.
4. Copy the Hubert GHA workflow templates into each
   watched repo's `.github/workflows/`; fill in the
   `inputs:` contract.
5. Write `.hubert/README.md` in each watched repo per
   [`.hubert/README.md.example`](.hubert/README.md.example).
6. Configure the Job template with the env vars
   documented in section [5](#5-image-contract).

The reference deployment uses `../hermetic`'s image and
`ekdromos` cluster; section
[7](#7-reference-deployment-hermetic) documents what you
get for free and what you'd need to replicate on a
different cluster.

---

## 10. Open design threads — mixed tiers

Threads where v1 commits to a direction but the specifics
need real design before or during implementation. Each has
a *Recommended direction* so this isn't a homework list —
it's a "we agreed to X, here's what still has to get built"
list.

### 10.1 OOM and failure post-mortem capture — **[Soon]**

**The problem.** When a pod OOMs or deadline-exceeds
mid-run, the pod dies before the runner can emit its final
decision record or post a clean escalation comment. We lose
the forensic trail for exactly the failure modes that most
need forensics.

**Recommended direction (v1).**

- **Incremental record emission.** The runner writes
  decision-record phase entries to the `hubert-log` repo
  *as each phase completes*, not only at exit. OOM loses
  the in-flight tool call but not the prior history.
  Primary defense.
- **Sibling cleanup Job on orchestrator-detected failure.**
  When the next orchestrator pass sees `kubectl get job`
  status as `Failed` and no decision record with outcome
  set, dispatch a small `hubert-cleanup` Job that
  inspects the last-written partial record, posts an
  escalation comment, and marks the run as OOM-terminated
  in the sink. Uses the same image but a different
  entrypoint; ~50 lines of Go.
- **`postStop` lifecycle hook (attempt, don't rely on).**
  Set a `preStop` hook that tries to flush the
  in-progress record. OOMkills don't call `preStop`
  reliably, so this is a best-effort belt; the sibling
  cleanup Job is the suspender.

**Explicitly v2.** OTel-collector sidecar streaming
tool-call events as they happen. Would give per-tool-call
resolution and catch in-flight-tool-call crashes too.
Good idea, not needed for v1 if incremental emission +
cleanup Job covers the common case.

**Improvements over the original proposal (from feedback).**

- **`kubectl get events` as part of cleanup.** Don't
  assume `Failed == OOM`. The cleanup Job runs
  `kubectl get events --field-selector
  involvedObject.name=<pod>` to collect the actual
  termination reason (OOMKilled, Evicted,
  DeadlineExceeded, Error) and the pod's last readable
  status before emitting the escalation comment. Without
  this, the recovery-pivot logic routes OOMs and
  deadline-exceeds identically, which is wrong — OOM
  wants `need-tier: larger`, deadline-exceeded with
  light memory use wants `need-backend: alternate` or
  the issue decomposed.
- **Deadline-minus-60s self-checkpoint.** The runner
  knows its `activeDeadlineSeconds` (passed in via env
  var or readable via the K8s downward API). Ship a
  watchdog goroutine that, at `deadline - 60s`, forces
  a checkpoint-and-commit regardless of whether the LLM
  has returned. Commits whatever the working tree
  contains, pushes, posts `🤖 hubert-run ... stopped
  reason=deadline-imminent`, and exits clean. Avoids
  the "LLM mid-think when kube pulls the plug" case —
  which is the one class of involuntary kill where
  *voluntary* checkpoint is possible.
- **Explicit honesty about heartbeat staleness.** On an
  OOM-mid-push, the last heartbeat may be minutes stale
  and the `run_id` may not have published a final
  comment at all. The orchestrator's recovery pivot
  table falls back to "unknown cause; start fresh with
  default tier" in that case — don't try to infer
  `need-tier: larger` from absence of signal, because
  the absence is more commonly "runner crashed before
  writing" than "runner ran out of memory."

### 10.2 Decision-record schema contract — **[Soon]**

**The problem.** §6 task 13 defines the record fields at a
high level. For v1, producers (runner) and the lone
consumer (humans doing `gh api` queries) can get by with a
loose schema. For v2 (Bloveate or purpose-built aggregator)
the schema needs to be stable and versioned.

**Recommended direction.**

- Ship v1 with `schema_version: "0"` on every record and
  a terse schema doc at `hubert-log/SCHEMA.md` in the
  sink repo. v0 is loose — producers can add fields;
  consumers branch on presence.
- Bump to `schema_version: "1"` when the v2 aggregator
  lands, with migrators in the aggregator (not in v1
  records).
- The schema doc is a living document in the sink repo,
  not in this plan — the producers write JSON and the
  schema follows.

**Open thread remaining.** Which fields are MUST vs SHOULD
at v0? A stricter v0 reduces v2 migrator pain. A looser v0
lets us discover fields we didn't know we needed. Pick
during v1 task 13 implementation — iterate on the first
few real records before locking.

### 10.3 Sink destination (`hubert-log` repo vs Bloveate) — **[Soon]** / **[Later]**

**The problem.** Where does the log land? `hubert-log`
repo is simplest (fits GitHub-as-state), Bloveate is
richer (query surface, observability tooling), and "both"
means producer complexity.

**Recommended direction.**

- **v1: `hubert-log` repo only.** Matches the
  GitHub-as-state philosophy; the producer is one
  `gh api PUT` call; ad-hoc `gh api` queries work against
  it. Zero additional infrastructure.
- **v2: Bloveate as an additional consumer.** Bloveate
  subscribes to `hubert-log` via repo webhooks, ingests
  records as they land, exposes a query surface. Hubert
  producers don't change. Adding a second producer path
  later (direct-to-Bloveate for performance) is a v3
  consideration if the GitHub-intermediate latency is
  ever a real problem, which it won't be at Hubert's
  volume.

### 10.4 CLI adapter interface specification — **[Soon]**

**The problem.** §6 task 10 sketches the `Adapter`
interface. The devil is in the `Event` shape: tool-call
events are CLI-specific in their current form, and
normalizing without losing information is a real
design task.

**Recommended direction.**

- Build the adapter incrementally: Claude first (the
  best-documented streaming JSON format), then OpenCode,
  then Gemini. Each adapter's first job is "make the
  runner work"; `Event` shape evolves as we add
  backends.
- Keep the interface **narrow**: `Phase`, `CostUSD`,
  `ToolCall{Name, Args, Result}`, `Output`, `Err`. If a
  CLI emits events that don't fit, log them as
  `Phase: "unknown"` with the raw JSON in a generic
  field — the runner doesn't need to understand every
  event to make progress.
- Model/pool selection lives in the orchestrator workflow
  (one config lookup per tick); the adapter only
  implements "invoke model X." Keeps the adapter
  portable across control-plane and execution-plane
  callers.

**Open thread remaining.** Whether the adapter should
normalize tool names (e.g., `claude.Bash` →
`tool.shell`) or pass them through. v1 answer: pass
through; normalization is a v2 concern if the decision
records show tool-name drift causing queryability
problems.

### 10.5 Idempotency-key details — **[Soon]**

**The problem.** §6 task 12 specifies SHA256 over
`(action_type | target_id | canonical_params_json)` and a
30-minute dedup window. Left open: what goes into
`canonical_params_json`; window tuning based on observed
false-dedup rate.

**Recommended direction.**

- **Canonical params:** all action fields *except*
  timestamps, `run_id` (generated per dispatch), and any
  advisory fields. Required fields for dispatch actions:
  `target_issue_or_pr`, `mode`, `agent`, `model`, `tier`,
  `iteration`. Sort keys, strip whitespace, UTF-8
  encode.
- **Window tuning:** 30 min at v1 launch. Log every
  dedup hit as a structured event in the workflow run
  summary. After two weeks of real use, review the
  dedup-hit log for false positives (different
  legitimate actions with colliding hashes) and false
  negatives (dupe races that made it through). Tune.
- **Bypass mechanism:** a `--no-dedupe` flag on
  `hubert-dispatch` for emergencies (human operator
  needs to force a re-dispatch). Never used by
  automation.

### 10.6 GHA backstop cadence at multi-repo scale — **[Soon]**

**The problem.** 10-min cadence is the v1 compromise. At
10+ watched repos it may still exhaust free-tier minutes;
at 2 repos it's comfortable.

**Recommended direction.**

- Start at 10 min, measure consumption at 2–3 watched
  repos for a month.
- If consumption approaches the free-tier ceiling,
  evaluate: (a) slow to 15 min (still fine — webhooks
  do the reactive work), (b) budget paid minutes
  (cheap at this volume), or (c) move the backstop to
  `actions-runner-controller` on the same cluster,
  giving us self-hosted GHA runners. Option (c)
  eliminates the budget concern entirely and is the
  right long-term answer if Hubert adoption grows.
- The backstop's only jobs are stale-lock reaping and
  catching missed webhooks; a 10-min delay on *those*
  is fine. Don't tighten the cadence for perceived
  responsiveness — webhooks handle that.

### 10.7 Branch-protection two-identity details — **[Soon]**

**The problem.** §3 "Why two bot identities" commits to
`hubert-exec-bot` + `hubert-review-bot`. Details left
open: exact PAT scopes, provisioning script, migration
to GitHub App.

**Recommended direction.**

- **PAT scopes at v1.**
  - `hubert-exec-bot`: `contents:write`,
    `pull-requests:write`, `issues:write` on watched
    repos (fine-grained PAT).
  - `hubert-review-bot`: `pull-requests:write`,
    `issues:write`, `contents:write` on watched repos
    (merge requires contents write).
  - `hubert-log-bot` (separate, task 13):
    `contents:write` on the `hubert-log` repo only.
- **Provisioning script:** a Bash script shipped with
  Hubert that walks an operator through the three-PAT
  creation, stores them in the designated K8s Secret
  and GHA Secrets. Idempotent — running twice does not
  create new PATs if existing ones are valid.
- **GitHub App migration (v2):** one GitHub App with
  two *installation configurations* (exec, review) on
  each watched repo. The App framework gives us
  short-lived tokens and collapses the PAT-rotation
  concern.

**Open thread remaining.** Whether `hubert-exec-bot` also
needs `workflows:write` in any scenario (to update
Hubert's own GHA workflow YAMLs in watched repos as the
templates evolve). v1 answer: no — workflow updates are
human-operator work, not Hubert work. Reconsider if
workflow-template drift becomes a real operational pain.

### 10.8 Reviewer proposes failing test — enforcement shape — **[Soon]**

**The problem.** §3 "Why LLMs agree is not correct"
commits to the reviewer proposing a failing test before
approving. Open: how enforced — soft (prompt rule) or
hard (runner checks)?

**Recommended direction.**

- **v1: prompt-level.** The reviewer prompt instructs
  the reviewer to include a proposed failing test in
  its approval comment, and the executor iterate-mode
  prompt instructs the executor to include that test
  in the PR. Not mechanically enforced; the reviewer
  can still approve without proposing a test (shouldn't,
  but can).
- **v1.5 or v2: mechanical.** Add a runner check in
  reviewer mode: before posting an approve review,
  verify the diff contains at least one added test
  file or line matching `*_test.go` / `*.test.ts` /
  `test_*.py` etc. Configurable per-project via
  `.hubert/README.md`. Enforcement at the runner
  boundary, not the prompt boundary.

Keep the soft version in v1 until we see real failure
modes — the prompt instruction may be sufficient; the
runner check is a belt if it's not.

### 10.9 Cross-repo budget pooling — **[Soon]** (deployment-cap) / **[Later]** (real pooling)

**The problem.** v1 tracks cost per-repo with a per-repo
daily cap. A single deployment watching 10 repos can end
up with 9 repos sitting at 5% of cap while one active repo
is pinned against its cap and refusing dispatches — even
though the *deployment* has plenty of budget left.
Per-repo caps are simple but they don't match how a
single-operator deployment actually pays for Anthropic
usage (one bill, one pool).

**Recommended direction.**

- **v1: per-repo caps only.** Keep the v1 cost model
  simple. A per-repo daily cap is a coarse guard against
  runaway spending in a single repo; that's the highest-
  value safety property and we can ship it alone.
- **v1.5 (optional): a deployment-level daily cap as a
  second ceiling.** Reads from a shared GHA organization
  secret or a config blob in `hubert-log`. Orchestrator
  refuses dispatches when *either* the per-repo cap or
  the deployment-wide cap is exhausted. Still no pooling
  — just two ceilings. Low-risk addition.
- **v2: real pooling via the central scheduler** (see
  § 8 "Central scheduler"). The scheduler holds the pool
  budget and allocates to repos by priority: repos with
  recent human activity, repos with `hubert-priority:*`
  labels, repos with smaller issue queues (likely to
  finish and free capacity). Per-repo caps become *soft*
  — advisory, overridable by scheduler policy. This is
  the real answer, but it requires the scheduler
  infrastructure to land first.

**The unresolved question.** Whether the scheduler's value
is *mostly* cross-repo budget pooling, or whether budget
pooling is a nice-to-have on top of the scheduler's other
coordination benefits (deferral, provider-health-aware
routing). If the former, adding a deployment-level cap to
v1.5 may capture most of the value without the scheduler.
If the latter, the scheduler is worth building for the
coordination reasons alone, and pooling is a bonus.

**How to decide.** After a month of multi-repo v1
operation, look at the per-repo cost distribution. If one
or two repos routinely cap out while others sit at
~10%, pooling matters and the scheduler case is strong.
If spend is spread evenly across repos, pooling is a
minor optimization and the scheduler should be
justified by the other properties alone (provider-health
awareness, deferral, reaction-based admission).

### 10.10 Prompt-portability CI — **[Soon]**

**The problem.** The design commits to backend-portable
prompts — the same orchestrator/executor/reviewer text
drives `claude`, `opencode`, and `gemini`. The commitment
has no enforcement. Claude, OpenCode, and Gemini differ
meaningfully in tool-use discipline, edit-loop semantics,
and self-correction patterns. Drift becomes visible only
when a production dispatch fails in a subtle way on a
non-Claude backend.

**Recommended direction.**

- **Fixture repo.** A small repo with a known set of
  seed issues (a trivial "add hello world" issue, a
  bug-fix issue with an intentional test, a scope-creep-
  bait issue, a decomposition-eligible issue) lives in
  `tests/fixtures/` in the Hubert repo.
- **Per-backend harness.** A CI workflow runs the
  orchestrator prompt, then the executor prompt, then
  the reviewer prompt against each configured backend
  (claude, opencode, gemini) on each fixture issue.
  Records the expected structural output (orchestrator
  emits a valid action list; executor opens a PR with
  the right shape; reviewer emits approve/request-
  changes with a proposed test).
- **Pass/fail criteria.** Structural only in v1 — does
  each prompt produce output the next stage can parse.
  Not "is the code correct." Semantic correctness is
  covered by the end-to-end test; portability is about
  the contract between stages holding across backends.
- **Drift visible in CI.** A backend that breaks the
  contract (a Gemini run that emits malformed JSON,
  an OpenCode run that ignores the "propose a test"
  instruction) fails CI, loudly. Operators don't find
  out in production.

This is cheap and it closes a real blind spot. [Soon]
rather than [Now] because the backend-portable claim
only matters once the second backend lands (task 10),
and the fixtures only become meaningful against real
prompts.

### 10.11 Snapshot-diet mode — **[Soon]**

**The problem.** The snapshot spec (all open issues + 5
comments each + all open PRs + 60min hashes + collaborators)
scales linearly with repo activity. A repo with 50 open PRs
produces a substantial token payload per tick, multiplied by
tick rate, paid every tick whether anything changed or not.

**Recommended direction.**

- **Diff-mode snapshot.** The snap helper tracks its last
  emission's content hash per item (issue, PR) in a small
  cache file in `hubert-log` or on a K8s volume. On each
  tick it produces:
  - Always: kill-switch state, collaborator list,
    daily_spend, recent_action_hashes, provider_health.
  - Changed-only: issues/PRs whose content hash differs
    from the last-tick cache.
  - Reference-only (no body): unchanged items listed by
    `{number, title, author, last_change_at}` so the
    orchestrator knows they exist but doesn't re-read
    their bodies.
- **Full-refresh triggers.** A scheduled daily full
  snapshot, and an explicit `/hubert refresh` comment
  from a committer, override diff-mode and emit the
  full payload. Catches drift if the cache falls out of
  sync with reality.
- **Orchestrator prompt aware.** The orchestrator prompt
  is told explicitly that unchanged items are reference-
  only and their detail is elided; if the orchestrator
  needs full detail on a reference-only item (rare),
  it emits a `need-full: issue=N` action and the next
  tick includes that item's body.

Not a [Now] concern — `corral` is small and single-repo;
the payload is trivial. Becomes real at ~5 repos or one
active repo with a high PR volume.

### 10.12 Per-tool-call timeout and egress cap — **[Soon]**

**The problem.** Cost caps are soft-by-one-tool-call. A
single adversarial or misbehaving tool call — a recursive
`find /`, a large `curl` download, a runaway `go build -v`
— can overshoot the per-run cap in one shot. The budget
cap catches it *after* the money is spent.

**Recommended direction.**

- **Per-tool-call wall-clock timeout.** The runner wraps
  every Bash invocation with a default 60-second timeout,
  configurable per-project in `.hubert/README.md` with
  a hard deployment ceiling of 5 minutes. Tools that
  exceed the timeout get SIGTERM, then SIGKILL 5 seconds
  later. A tool-level timeout is cheaper and more
  predictable than a budget-level check that runs after
  the cost is already spent.
- **Per-tool-call egress byte cap.** Bash invocations
  run inside a cgroup with an outbound byte-rate cap
  (default 10 MB/minute, tunable). Catches the runaway-
  download class. Implementable via `tc` or
  `systemd-run --property=IPAccounting=yes` in the
  runner's pod spec.
- **Explicit allow-list for long tools.** If a project
  legitimately needs a 20-minute integration-test run,
  it whitelists `go test ./integration/...` in
  `.hubert/README.md` under a `long_tools:` block. The
  runner honors the whitelist and skips the default
  timeout for matching invocations.

Addresses the class of failure where an unbounded single
call eats the budget before the cap can react. Does not
replace the budget cap — it complements it.

### 10.13 Issue-derived test harness — **[Later]**

**The motivation.** Two LLMs agreeing on a diff is not
the same as the diff being correct. The reviewer-proposes-
failing-test rule (§ 10.8) is a partial defense, but it
only catches bugs the reviewer remembered to test for.
Reviewer and executor have shared training-data blind
spots; a bug class both models miss at *test-writing*
time is the one that slips through.

**The mechanism.** A third agent — the **test-deriver**
— that sees only the issue body (never the diff, never
the executor's reasoning, never the reviewer's proposed
test). Its prompt: "Propose the tests that must pass for
the issue to be considered resolved." Outputs a test
specification (framework, file location, test names,
assertion shapes).

The reviewer then runs the executor's code against the
test-deriver's tests. If they pass, that's real evidence
— the test-deriver never saw the proposed solution and
therefore didn't co-adapt to its blind spots. If they
fail, the executor iterates.

**Why this is the real answer to shared training data.**
"Different backend for the reviewer" catches per-call
variance; "test-deriver saw only the issue" catches
systemic shared blind spots. The test-deriver is the
closest LLM-era analogue to TDD written by a separate
engineer.

**Deferred to [Later] because:** it requires the reviewer
agent to exist first (task 15a) and it triples the
per-issue LLM budget. Worth it once the auto-merge path
is being used seriously; premature at [Now] or [Soon]
scale.

**Composes with:** § 10.8 enforcement shape (test-deriver
output *is* the proposed failing test, removing the "did
the reviewer propose a test" question); § 10.18
structural review diversification (test-deriver is one
role in a multi-role review).

### 10.14 Reviewer-as-adversary framing — **[Soon]**

**The problem.** The reviewer prompt currently asks
"does this look right?" — a framing that biases toward
approving changes that don't trip any specific alarm.
Adversarial framing catches different failure modes.

**Recommended direction.** A prompt-level change to the
reviewer: in addition to the existing approval criteria,
instruct the reviewer to "find the worst input that
breaks this change." Adversarial outputs become proposed
breaking inputs, and those become tests in the iteration
request.

Cheap to ship (prompt-only), composes with § 10.13 (the
adversary's breaking inputs feed the test-deriver
naturally), and catches a different class of bug than
the existing "propose a failing test" rule.

### 10.15 Post-merge observation window — **[Later]**

**The problem.** After a Hubert merge, the system goes
silent. But the ground-truth signal for "was the
reviewer right?" only materializes in the days/weeks
after — a revert, a follow-up issue citing the PR, a
test added later that would have failed pre-merge, an
explicit human comment flagging a regression. Today
those signals vanish into GitHub without feeding back.

**Recommended direction.** For N days after a Hubert
merge (default N = 14), the orchestrator watches for:

- The merge getting reverted (`git revert` on the merge
  commit, or a revert PR).
- A follow-up issue whose body or title references the
  merged PR number.
- A later commit adding a test that would have failed
  pre-merge (detected via running the added test
  against the pre-merge SHA).
- A human `@hubert-is-a-bot` comment flagging a regression.
- A human-authored commit that modifies files touched
  by the Hubert merge within the window.

Each signal is logged as a structured decision-record
supplement (`run_id`, signal type, context). Over time
these supplements are the only non-LLM ground-truth
signal in the system — everything else is grading LLM
output with LLM output.

**Why this is the highest-value [Later] addition.** The
replay harness (§ 10.17) and the cross-repo pattern
extraction (§ 10.19) both need a ground-truth signal to
calibrate against. Post-merge observation *is* that
signal. Without it, every prompt tweak is a guess.

### 10.16 Human-in-the-loop as a gradient — **[Soon]**

**The problem.** The original shadow-mode → auto-merge
graduation is binary: either the reviewer merges or it
doesn't, globally. That's wrong for real codebases where
some paths are genuinely fine to auto-merge (doc
updates, test-only changes) and others really shouldn't
be auto-merged at all (auth, secrets, schema, public
API, dependency updates, CI config).

**Recommended direction.** Replace the binary with a
three-level policy surface driven by path globs in
`.hubert/README.md`:

```
# .hubert/README.md
hitl_gradient:
  auto_merge:
    - "docs/**"
    - "**/*.md"
    - "tests/unit/**"
  shadow_mode:
    - "internal/**"
    - "cmd/**"
  human_required:
    - ".github/**"
    - "**/auth/**"
    - "**/migrations/**"
    - "go.mod"
    - "go.sum"
```

- **`auto_merge`** — reviewer approves and merges. The
  auto-merge graduation criterion (§ 15a) applies per
  glob class.
- **`shadow_mode`** — reviewer approves; a human clicks
  Merge. Default state for paths that have not yet
  earned auto-merge.
- **`human_required`** — Hubert refuses to dispatch
  execution at all on issues whose natural scope
  includes these paths. Orchestrator emits
  `escalate(reason="path-class requires human")`.

Labels can override paths: a `hubert-auto-merge` label
on an individual PR force-promotes to auto-merge;
`hubert-human-required` force-demotes.

**Why [Soon] rather than [Later].** This is the single
architectural change that makes auto-merge safe to ship
at all. The original "corral for 2 weeks, then the gates
open" plan was too coarse; the HITL gradient is what
makes real-codebase adoption possible without an
all-or-nothing trust jump.

**Composes with:** § 10.8 (test enforcement applies
per-class); § 10.15 (post-merge regressions on an
auto-merge class demote it back to shadow).

### 10.17 Capability negotiation — **[Soon]**

**The problem.** The orchestrator currently *predicts*
agent/model/tier up front; the executor confirms or
escalates. When the prediction is wrong, the executor
bounces (OOM, deadline, rate-limit, "too hard for this
model") and the next orchestrator tick pivots. This
works but it's lossy — the orchestrator keeps guessing
with imperfect information.

**Recommended direction.** Promote "insufficient-
capability: need X" from a failure recovery path to a
first-class response type. The executor emits
`capability-need(tier=large, reason="diff is 800 lines
across 5 files, cheap model stuck in compile errors")`
*before* committing to a specific tier. The orchestrator
re-dispatches with the elevated tier in the next tick
rather than waiting for a timeout or OOM to reveal the
mismatch.

Small change to the action schema (new action type);
bigger change to the orchestrator prompt (must read the
capability-need comments on issues). Implementation is
incremental: the executor starts emitting the hints in
the first version, the orchestrator starts reading them
in the next.

**Composes with:** § 10.16 (HITL gradient rules can
include "auto-merge only if the executor reported
tier=small was sufficient" — tier-inflation becomes a
scope-creep signal).

### 10.18 Structural review diversification — **[Later]**

**The motivation.** One generalist reviewer is one shape
of cognitive bias. Parallel specialized reviewers — a
security-review, a scope-review, a test-coverage-review,
a data-shape-review — catch different failure modes
because they're looking for different things, not just
looking through different model weights.

**Recommended direction.** Instead of one reviewer pass
that applies all criteria, fan out the PR to N small
reviewers each with a focused prompt and narrow context.
Aggregation is a meta-pass that combines their outputs
into a single review.

- `security-review`: scans for hardcoded secrets,
  command injection, deserialization of untrusted input,
  SQL injection.
- `scope-review`: compares the diff to the issue, flags
  scope creep or under-delivery.
- `test-coverage-review`: checks that the diff includes
  tests at the level the surrounding code requires.
- `data-shape-review`: for diffs that touch
  serialization, schemas, or external APIs — checks
  wire-format compatibility.

Each small reviewer is cheap (focused prompt, small
context). The meta-pass is also cheap (it's combining
structured outputs, not re-reading the diff).

**Composes with:** § 8 v2 agent personas (a
security-auditor persona *is* the security-review
agent); § 10.13 (the test-deriver feeds
test-coverage-review).

### 10.19 Confidence-weighted approval — **[Later]**

Reviewer reports confidence (0.0–1.0 or low/medium/high)
alongside approve/reject. Low-confidence approvals
escalate to human regardless of auto-merge policy.
Confidence is calibrated over time via the post-merge
observation window (§ 10.15) — confidence scores that
correlate with post-merge incident rate are kept;
confidence scores that don't correlate are discarded.

Prompt-level change for v1; real calibration requires
the observation window, which is [Later].

### 10.20 Issue-author feedback loop — **[Later]**

When a trusted committer reacts negatively to a merged
Hubert PR — via revert, follow-up issue citing the PR,
or explicit `@hubert-is-a-bot this was wrong` comment — that
signal is a first-class input to reviewer calibration.
Doesn't require special annotation; uses the project's
existing workflow signals. Foundational for the
reviewer-improvement story.

**Composes with:** § 10.15 observation window (provides
the detection mechanism); § 10.19 confidence-weighted
approval (calibration target).

### 10.21 Partial-credit merge outcomes — **[Later]**

Currently the reviewer's options are binary: approve and
merge the whole PR, or request changes on the whole PR.
A common real-world case is "90% of this is fine, this
one function is wrong." Give the reviewer a third
option: merge the approved portions and reopen the
unapproved portions as follow-up issues with
`decomposition-depth: parent+1`.

Requires non-trivial machinery (splitting a branch,
filing sub-issues, re-linking). [Later] because the
iteration path (§ 6 iterate mode) covers the common case
adequately; partial-credit is a natural upgrade once
decomposition is being used often.

**Composes with:** § 8 agent personas (the persona
distinction may make "this function belongs to
schema-migrator not backend" a natural split); iteration
cap (partial-credit closes out iterations that would
otherwise hit the cap unproductively).

### 10.22 Replay-based regression harness — **[Later]**

Every successfully merged Hubert PR becomes a test
fixture: `(issue-body, repo-state-before, merged-diff,
test-outcomes)`. Changes to orchestrator prompts,
reviewer prompts, or routing policy can be regression-
tested against the fixture corpus deterministically —
re-run the orchestrator on each fixture's snapshot,
check that its action list matches; re-run the executor
prompt against each fixture's before-state, check that
its diff still passes the fixture's tests.

**Why this is the missing piece for iterating on
prompts without fear.** Today a prompt tweak is a guess
— the only validation is "run it on a new issue and
see." With a fixture corpus, prompt changes have a
regression signal against a growing corpus of known-good
runs.

**Composes with:** § 10.13 (test-deriver outputs are
fixture tests); § 10.15 (observation-window signals
become fixture annotations — "this PR was reverted,
don't use it as a positive fixture").

### 10.23 Cross-repo pattern extraction — **[Later]**

Once decision records accumulate across repos, patterns
surface: "this executor consistently mishandles Go
generics," "schema-migrator persona never writes the
down migration when the issue title contains 'drop',"
"Gemini dispatches on issues with attachments fail at
3× the rate." These are actionable — they feed persona
tuning, orchestrator routing policy, and backend choice.

Emerges from the decision-record aggregator (§ 10.3 v2)
plus a lightweight pattern-miner agent. Natural
composition with § 8 agent personas (persona tuning is
the primary output) and § 10.19 confidence calibration
(miner surfaces patterns that correlate with low
confidence).

### 10.24 Decision-record completeness as graded substrate — **[Someday]**

Current task 13 treats decision records as per-run
artifacts. Higher-value framing: treat the
**completeness of the decision-record corpus** as a
graded substrate. A PR merged with a record that omits
"tests run with results" is lower-quality substrate than
one with a full record; over time, incomplete records
get backfilled by later runs referencing the same
issue, the same files, or the same persona.

Speculative — the value is in what it enables
downstream (higher-quality replay fixtures, more
reliable pattern mining) rather than in any single v2
feature. Captured here so the direction isn't lost when
designing the v2 aggregator.

---

## 11. What v1 explicitly defers

Canonical index of everything v1 chooses not to ship. If
you see a feature you expected in v1 and it's not in the
[Now] or [Soon] bands of § 6, it's either here or it's a
bug in this index — open an issue.

**Deferred to [Soon] (earned by the [Now] loop working):**

- CLI adapter abstraction — § 6 task 10.
- Recovery-pivot flow with the full table — § 6 task 8.
- Action idempotency (race-closed label-based
  implementation) — § 6 task 12 + § 10.5.
- Three-tier kill switch (global + per-repo + per-issue)
  — § 6 task 7 [Soon] expansion.
- Decision records + `hubert-log` sink — § 6 task 13.
- Provider-health record — § 6 task 14.
- Structured JSON logging — § 6 task 9.
- Reviewer agent with auto-merge (gated by HITL
  gradient) — § 6 task 15a + § 10.16.
- Shadow-merge graduation with volume+diversity
  criterion — § 6 task 15a.
- Prompt-portability CI — § 10.10.
- Snapshot-diet mode — § 10.11.
- Per-tool-call timeout + egress cap — § 10.12.
- Reviewer-as-adversary framing — § 10.14.
- HITL gradient — § 10.16.
- Capability negotiation — § 10.17.

**Deferred to [Later] (v2 material, requires [Soon]
infrastructure first):**

- Agent personas — § 8 v2.
- Central scheduler with reaction-based signaling —
  § 8 v2.
- GitHub App migration (single App with role
  installations) — § 8 v2.
- Aggregated decision-record query surface (Bloveate or
  purpose-built) — § 10.3.
- Issue-derived test harness — § 10.13.
- Post-merge observation window — § 10.15.
- Structural review diversification — § 10.18.
- Confidence-weighted approval — § 10.19.
- Issue-author feedback loop — § 10.20.
- Partial-credit merge outcomes — § 10.21.
- Replay-based regression harness — § 10.22.
- Cross-repo pattern extraction — § 10.23.
- Cross-repo budget pooling (real pooling) — § 10.9.

**Deferred to [Someday] (v3 / vision):**

- Per-role GitHub Apps (coder / reviewer / researcher)
  — § 8 v3.
- Open-source bot: attachment classifier, "lift"
  mechanism — § 8 v3.
- Persona marketplace — § 8 v3.
- Central scheduler with reaction primitive as platform
  for `agency-agents` integration — § 8 v3.
- Decision-record completeness as graded substrate —
  § 10.24.

**Explicitly rejected (never in any tier):**

- A web UI. GitHub is the UI.
- Resuming a prior agent session across Jobs.
  Checkpoint-and-exit starts fresh by design.
- Mocking the build/test/lint step. Real CI runs; red
  CI is never approved.
- Bypass mechanisms for required approvals
  (`--force-merge`, `--skip-review`). The reviewer
  gate is load-bearing.
- Honcho memory integration as currently specified.
  Paused; revisit only if decision-record aggregation
  surfaces a concrete memory need.

---

## Appendix A: Building v1 — the multi-agent work plan — meta-plan

> **Framing.** This appendix is **not a product tier.** It
> describes how the humans + agents driving the build
> execute against the plan — pool topology, fan-out
> strategy, review gates, commit hygiene. The product
> tiers ([Now]/[Soon]/[Later]/[Someday]) are in §§ 6, 8,
> 10. This appendix tells the builder how to *get to*
> those tiers, not what they contain.

v1 is not a solo-Claude-Opus implementation. Hubert is the
kind of thing that *has* to work — wrong merges, leaked
secrets, silent scope reduction, all of it is high-cost
failure — and a single-model single-pass build is exactly
the shape of project most likely to ship a quiet bug. The
implementation strategy mirrors the product strategy:
**many agents, cross-checking each other, with explicit
review gates.**

This section documents *how* we build v1, not *what* v1
is. It's the instructions to whichever orchestrator (human
or agent) is driving the build — including a future run
of this very Claude Code session after context compression.

### A.1 Pool topology

Four independent token pools participate:

| Pool          | CLIs / invocation                                      | Best-for                                           |
| ------------- | ------------------------------------------------------ | -------------------------------------------------- |
| **Anthropic** | `claude` (this session + Agent-tool sub-agents)        | Orchestration, design, high-judgment review        |
| **Google**    | `flatpak-spawn --host gemini -y -p ...`                | Web research, URL verification, spec lookups       |
| **OpenAI**    | `flatpak-spawn --host opencode run -m openai/... ...`  | Code writing (Codex variants), flagship refactors  |
| **Free pools** | `opencode -m opencode/big-pickle`, OpenRouter `:free` | Bulk code writing, cross-check review, TDD loops   |

**Pool assignment principle:** the orchestrator (this
Claude session) is the scarcest resource because it's
also funding the coordination overhead. Delegate
aggressively. Leaf implementation work (write this Go
file, add this test, verify this build passes) goes to
free or cheap backends first. Keep Anthropic tokens for
design decisions, synthesis, and the final review pass.

**Model selection** per the existing model-tiering memory:

- Fact-gathering / file-reading / inventory → `opencode/big-pickle`, `opencode/nemotron-3-super-free`, or `opencode/gpt-5-nano`.
- Code generation with clear specs → `openrouter/qwen/qwen3-coder:free`, `openrouter/moonshotai/kimi-k2:free`, `opencode/nemotron-3-super-free`.
- Multi-file refactors, careful code → `openai/gpt-5.4`, `openai/gpt-5.3-codex`.
- Architecture, algorithm design, tradeoff reasoning → `openai/gpt-5.1-codex-max` or Claude Opus via Agent.
- Web research with source verification → `gemini-3-flash-preview` (default), `gemini-3.1-pro-preview` (deep).
- URL verification / HTTP probing → any free model with bash access.

See
[`~/.claude/memory/shared/reference_orchestration_playbook.md`](../../../../.claude/memory/shared/reference_orchestration_playbook.md)
for the full dispatch playbook; it is the source of truth
for CLI invocation patterns, sandbox rules, and the
`.tmp/prompts/` discipline.

### A.2 Parallel fan-out

Target concurrency: **4–5 leaf tasks in flight**. The
orchestrator (this Claude session) dispatches:

- Independent tasks fan out in a single message with
  multiple Agent-tool calls (for Claude sub-agents) or
  multiple Bash calls (for headless `opencode run` /
  `gemini -p` invocations, backgrounded).
- Dependent tasks serialize naturally — the orchestrator
  waits for the upstream to return before dispatching
  the downstream.
- The 4–5 ceiling is soft and set by the orchestrator's
  ability to synthesize returning results without losing
  coherence. Push higher when results are mechanical
  (bulk file edits, TDD loops); pull lower when results
  require careful reconciliation (design proposals,
  trade-off comparisons).

### A.3 Review gates — every change reviewed by a different model

No code enters the repo until it has been **written by
one backend and reviewed by another**. The review isn't
a code-stylistics pass; it's a correctness-and-
completeness pass of exactly the shape §3 "Why LLMs agree
is not correct" commits to for Hubert itself. The two-
backend rule is deliberate: same-backend review
cross-contaminates the hypothesis space. If OpenCode
writes the runner, Claude or Gemini reviews it — not
another OpenCode run.

Specific gates:

1. **Compile gate.** Every code-writing dispatch must
   include the compile-gate preamble from
   [`reference_subagent_preamble.md`](../../../../.claude/memory/shared/reference_subagent_preamble.md).
   Sub-agents don't report completion until `make build`
   passes. Non-negotiable.
2. **Test gate.** For any change adding or modifying
   behavior, the sub-agent runs the existing test suite
   and adds a test that would have caught the bug (or
   exercises the new feature end-to-end) before
   reporting.
3. **Cross-backend review gate.** The orchestrator
   dispatches a review pass to a *different* backend
   with the diff + the original spec. Reviewer returns
   approve / request-changes / escalate. Iterate loop
   bounded at 3 cycles; escalate to human at cap.
4. **Security review gate.** For anything touching
   auth, secrets, GitHub API writes, or the trust gate,
   run an additional security-focused review pass on a
   frontier model (`openai/gpt-5.1-codex-max` or Claude
   Opus). Cheap backends are fine for bulk work; trust-
   adjacent code earns the expensive second look.

### A.4 Session resume discipline

Long build chains benefit from session resume — picking
up the agent's prior context rather than re-briefing from
scratch. Resume semantics per backend:

- **Claude Code**: `-r <uuid>` or `--fork-session`. Template
  sessions per
  [`reference_orchestration_playbook.md`](../../../../.claude/memory/shared/reference_orchestration_playbook.md)
  §"Template Sessions".
- **OpenCode**: `--session=<id>` to resume, `--fork` to
  branch.
- **Gemini**: `-r <index|latest|tag>` to resume,
  `/resume save <tag>` to checkpoint.

Rule: resume when the follow-up is surgical and depends
on prior context; start fresh when the follow-up is a
distinct task. Re-explaining from scratch in a resumed
session is a cost bug.

### A.5 Scratch discipline and prompt delivery

All sub-agent prompts go through files, not inline
strings:

- Write prompts to `.tmp/prompts/<task>.txt` in the
  project root.
- Dispatch with `$(cat .tmp/prompts/<task>.txt)`
  expansion (or `-f .tmp/prompts/<task>.txt` where the
  CLI supports it).
- Scratch files, debug scripts, and intermediate
  artifacts go in `.tmp/` as well — never `/tmp` (the
  flatpak sandbox can't reach `/tmp`, and headless
  OpenCode denies external-directory writes).

Every sub-agent prompt includes the scratch-dir
instruction verbatim; without it, agents default to
`/tmp` and the run dies on the first tool call.

### A.6 Commit and review hygiene

Per
[`feedback_commit_practices.md`](../../../../.claude/memory/shared/feedback_commit_practices.md):

- **Conventional Commits** with package-scoped types
  (`feat(runner):`, `fix(cliadapter):`, etc.).
- **Co-author every AI model** that materially
  contributed to a commit, by name and provider. A
  review pass that approved the change is a contributor.
- **Commit incrementally** — one logical change per
  commit. Makes review-pass dispatches tractable.
- **Never amend** unless explicitly asked. The
  pre-commit hook may fail; fix and re-commit fresh.

### A.7 Meta-review loop

Even with all the above, a single agent (including this
one) can drift. **At every sub-milestone** — end of each
§6 task — the orchestrator pauses and dispatches a
*fresh* review pass on a different backend with the spec
+ the diff + the prior reviewers' notes, asking "does
this task match the spec? Any corners cut? Anything
missing?" The meta-reviewer has no session history from
the build itself; its fresh context is the point.

Cost-cheap: the diff is small, the spec is stable, the
prompt is short. Pattern-catching: a fresh reader spots
things the orchestrator has become blind to. This is the
same discipline as the production reviewer agent
(§3 "Why LLMs agree is not correct") applied to our own
build work.

### A.8 Escalation to human

Escalate to the operator (Evan) when:

- Cross-backend reviewers disagree after 3 iteration
  cycles with no convergence.
- A trust-adjacent change (auth, secrets, branch
  protection, trust-gate logic) gets any negative signal
  from any review pass — human confirmation before merge,
  not another LLM pass.
- The compile or test gate fails in a way that suggests
  a spec problem, not an implementation problem.
- The prereq in §6 "Prerequisites" is blocking (Docker
  not working, PATs not provisioned, `corral` repo not
  set up) and the orchestrator can't resolve it without
  operator action.
- Cost consumption approaches the session's daily
  budget. Protect the orchestrator pool first — without
  it, the whole build stops.

Escalation is not failure. Escalation is the orchestrator
doing its actual job: recognizing when *it* is the wrong
tool.
