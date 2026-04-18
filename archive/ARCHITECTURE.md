# Hubert architecture

> Status: research-phase design. No code written yet. This document
> records the architectural decisions and the reasoning for each.
> If you want the threat model, see `SECURITY.md`. If you want to
> start building, see `PLAN-v1.md`. If you want the prompts that do
> the actual work, see `prompts/`. If you want what the execution
> substrate already provides, see `HERMETIC-HANDOFF.md`.

## One-paragraph summary

Hubert is a two-plane system. The **control plane** is a set of
short-lived GitHub Actions workflows — webhook-triggered for
reactive work (issue opened, PR `ready_for_review`, `check_run`
completed) and a tight `schedule:` (~2 minutes) for time-based
sweeps. Each workflow runs an **orchestrator** pass (one-shot
Claude Code invocation) that reads the current GitHub state and
decides what to do next, then either acts inline (labels,
comments, cheap orchestration) or submits a Kubernetes Job to
the **execution plane** on the `ekdromos` GKE cluster. Execution
and reviewer agents run as K8s Jobs at configurable resource
tiers, clone fresh, invoke the orchestrator-selected LLM CLI
(`claude`, `opencode`, or `gemini`) against an embedded
execution or reviewer prompt, push a branch, and post
comments. Those pushes and comments fire
new webhooks, which re-enter the control plane. The two planes
communicate only through GitHub state — GitHub holds the queue, the
lock, the audit log, and the UI. An execution agent that needs more
memory or a better model commits what it has, posts a structured
escalation comment, and exits cleanly; the next orchestrator pass
queues a fresh Job at the requested tier. No nested jobs, no
daemon, no local state.

## The system in five components

1. **Control-plane workflows (GHA).** A small set of workflow YAMLs
   in `.github/workflows/` in each watched repo. Triggers:
   `issues`, `issue_comment`, `pull_request`,
   `pull_request_review`, `check_run`, `schedule` (~2 min),
   `workflow_dispatch`. Each workflow is cheap: build the per-repo
   GitHub state snapshot, run the orchestrator pass, and either
   act inline (label, comment, `noop`) or invoke the Job-submission
   helper to dispatch execution/reviewer Jobs on the cluster. No
   heavy work runs inside GHA itself.
2. **Orchestrator pass.** A `claude --print` invocation against
   `prompts/orchestrator.md`, fed the per-repo snapshot. Planning-
   only, no Bash/Edit tools. Outputs a structured action list:
   `dispatch-execution(issue=N, tier=T, agent=A, model=M)`,
   `dispatch-reviewer(pr=M, agent=A, model=M)`,
   `reap-stale-lock(issue=N, job=X)`, `escalate(issue=N, reason=…)`,
   `noop`. Short, cheap, side-effect-free except for token spend
   and any label/comment writes the workflow applies on its behalf.
   The orchestrator stays on Claude Code because planning quality
   matters and the pass is short; execution and reviewer work can
   be routed to cheaper backends (see "Why three CLIs, not one"
   below).
3. **Execution agent.** The Hubert **runner binary**
   (`cmd/hubert-runner`) running inside a Kubernetes Job submitted
   via the hermetic infrastructure at `../hermetic`. The runner
   clones the repo, acquires the GitHub lock (assignment +
   heartbeat comment), invokes the orchestrator-selected LLM CLI
   (`claude`, `opencode`, or `gemini`) with the embedded
   execution prompt, pushes a branch, releases the lock, and
   exits. If it hits a constraint it can't solve at its current
   tier (memory, model quality, time), it commits what it has,
   posts a structured escalation comment, and exits cleanly for
   the next orchestrator pass to re-queue.
4. **Reviewer agent.** Same Job shape as the executor, typically a
   smaller tier. Reads a PR, evaluates it for quality /
   completeness / scope-fidelity, reads the CI status checks,
   posts a review comment, and either merges (on approval) or
   labels the PR `hubert-changes-requested`. Separate process from
   the executor with fresh context — a real second-opinion pattern,
   not a self-review rubber stamp.
5. **GitHub.** The state store, the lock manager, the queue, the
   event bus (via webhooks), the audit log, and the user-facing
   UI. Hubert owns nothing GitHub doesn't already own.

The hermetic infrastructure (K8s Job template, admission policy,
namespaced RBAC, Workload Identity) is a substrate that execution
and reviewer agents run *on*, not a component Hubert implements
from scratch. See [`HERMETIC-HANDOFF.md`](HERMETIC-HANDOFF.md) for
what ports and what doesn't.

## Decisions, with rationale

### Why a two-plane architecture (GHA control + K8s execution)

The first pass at this design was a single stateless Go binary
that ran on cron ticks and spawned subprocesses for execution.
Two follow-up conversations pulled the shape apart along the
natural seam: *deciding* what to do is cheap, fast,
webhook-reactive work; *doing* the work is expensive,
resource-heavy, slow work. Giving each half its own runtime lets
each be right-sized:

- **Control plane wants low-latency event triggers.** Webhooks
  fire on `issue.opened`, `pull_request.ready_for_review`,
  `check_run.completed` the moment those events happen. That's
  strictly better than the 15-minute poll the cron-binary design
  settled for. GHA is the only place we can consume webhooks
  without standing up our own webhook receiver.
- **Execution plane wants resource isolation and custom tiers.**
  GHA-hosted runners are fixed-size (7GB memory, 2 vCPU on the
  free tier, 6h max). A non-trivial refactor wants more memory
  than that, and an agent that runs for hours benefits from being
  killable independently of the control loop. K8s Jobs on hermetic
  give us the admission-policy-enforced tiers (small / medium /
  large / xlarge, up to 32Gi and 6h) that are already built.
- **The two planes don't need to share a runtime.** They
  communicate exclusively through GitHub state — commits, PRs,
  comments, labels, status checks. An executor's `git push` fires
  a webhook that re-enters the control plane on the next
  orchestrator pass. This is the same mechanism the bot already
  uses to coordinate with humans; there's no second channel to
  design.

The cost is a tighter coupling to two specific vendors: we're now
GHA-shaped on the control side and GKE-shaped on the execution
side. The mitigation is that both are operated by someone else —
GitHub runs GHA, hermetic is already running on `ekdromos` for the
hermes-agent deployment — and the control-plane workflows are
standard GHA YAML that moves to any webhook substrate if needed.
The execution plane uses vanilla K8s primitives (Jobs, ConfigMaps,
emptyDir), not App Engine or Pub/Sub, so it ports to any K8s
cluster.

### Why GitHub Actions as the control plane

Alternatives considered and rejected:

- **Cron/systemd on a laptop or VPS.** The shape of the original
  design. Works, but it's a supervision tree we'd have to own,
  and the polling window (15 min to stay inside rate limits) is
  much coarser than webhooks give us for free.
- **Google App Engine + Pub/Sub.** A real option given the
  existing GCP footprint, but it commits us to more GCP-specific
  surface (App Engine runtimes, Pub/Sub subscriptions, IAM
  bindings for the pull worker) for something GHA does with no
  additional infrastructure.
- **`actions-runner-controller` on the same GKE cluster.** Runs
  GHA workflows on K8s pods — the right answer *if* we wanted one
  runtime. But we don't, because the two planes want different
  things and ARC is another controller to operate. Worth
  revisiting only if GHA-hosted free-tier minutes become a real
  constraint.

GHA wins for the control plane because:

- The webhook-to-workflow trigger mapping is the thing we'd
  otherwise have to build.
- `GITHUB_TOKEN` is auto-provisioned with per-repo scope; no PAT
  to manage for control-plane calls.
- The `concurrency:` keyword handles dedup of overlapping event
  fan-out natively.
- The workflow YAML is *also* where the CI quality checks live
  (see "Why CI is the primary quality layer"), so the control
  plane and the quality plane share a config substrate.
- Free-tier minutes are generous for short orchestrator passes;
  we pay the real compute bill on hermetic, not GHA.

The known weak spot is that GHA's `schedule:` trigger is
unreliable — delays of 15+ minutes and occasional silent skips are
documented behavior. The mitigation is that the schedule is a
*backstop*, not the primary trigger: webhook events fire on every
real state change, and the scheduled sweep only catches stale
locks, missed webhook deliveries, and anything else that slipped
through. A 15-minute delay in reaping is not user-visible; a
15-minute delay in picking up a new issue would be, and webhooks
solve that one directly.

### Why ~2 minute scheduled ticks

The old design budgeted 96 orchestrator passes per day (one per
15 min). In the two-plane model the schedule is a backstop, so it
can be much tighter at negligible cost:

- Each scheduled tick is a short orchestrator pass — tens of
  seconds, no source checkout, no heavy model calls.
- The pass is a no-op when nothing has changed, which will be the
  case for most ticks.
- GHA free-tier minutes are plenty for 2-minute ticks across the
  repos Hubert will realistically watch in v1.

Tighter ticks mean faster recovery from missed webhooks and faster
reaping of dead Jobs, at no meaningful cost. Acknowledging GHA
schedule flakiness, "~2 minutes" will in practice mean "often 2,
sometimes 5, occasionally 15+" — still fine for the backstop role.

### Why K8s Jobs (via hermetic) instead of GHA-hosted runners

- **Resource tiers.** hermetic already defines small / medium /
  large / xlarge tiers (1Gi–16Gi memory, 1h–6h deadlines) with a
  `ValidatingAdmissionPolicy` enforcing ceilings at admission
  time. GHA-hosted runners are one fixed size on the free tier.
- **Isolation.** Each K8s Job runs in its own pod with namespaced
  RBAC, an emptyDir filesystem, and no cross-pod state. Admission
  policy rejects `hostNetwork`, `hostPID`, or privileged
  containers. hermetic has done this work; reusing it is label
  renames, not new infrastructure.
- **Independent killability.** An executor that misbehaves or runs
  too long is a `kubectl delete job/<name>` away from stopped. In
  the GHA-hosted model, killing a runner means canceling the
  workflow run — which also kills the control-plane loop that
  submitted it.
- **The work already exists.** The Job submission Go binary
  (`hermes-delegate/k8s.go`, ~400 lines), the admission policy,
  the RBAC, and the Workload Identity wiring all port into Hubert
  with label renames. See [`HERMETIC-HANDOFF.md`](HERMETIC-HANDOFF.md)
  for the port inventory.

### Why checkpoint-and-exit for escalation

An execution agent that finds it needs more resources — more
memory, a better model, more time — could block and wait, spawn a
bigger child and wait, or make a synchronous request over RPC to
an orchestrator. All of those couple the two planes.

The alternative we're taking is **checkpoint and exit**: the agent
commits whatever work it has to the feature branch, pushes, posts
a structured comment naming the constraint (`need-tier: large`,
`need-model: opus`, or free-form reason), and exits cleanly. The
next orchestrator pass reads the comment, queues a fresh Job at
the requested tier, and the new Job picks up at HEAD of the branch
with the comment trail as continuation context.

This falls out of the design:

- It reuses the same communication channel every other piece of
  the system uses: `git push` and a comment.
- It sidesteps nested-job deadlocks — nothing ever waits on
  anything it submitted. The submitter always exits before its
  child runs.
- It handles voluntary ("I can tell this is too hard for Sonnet")
  and involuntary (OOMkilled, deadline exceeded) escalation via
  the same mechanism. For involuntary, the orchestrator sees the
  pod's terminal state via `kubectl get job` and treats the
  failure as an implicit escalation request.
- It composes with the trust model: the escalation is a comment
  from `hubert-bot`, which the origination trust check already
  knows to treat as trusted.

The constraint it introduces is a per-issue escalation budget: an
agent that keeps asking for more, gets promoted to xlarge+opus,
and still can't make progress needs to be kicked up to a human,
not escalated again. The orchestrator enforces the cap.

Voluntary escalation is more reliable than involuntary. OOM in
particular is hard to checkpoint *from* — by the time you're
OOMkilled you may not have room to `git push`. Pair the pattern
with conservative up-front tier selection and treat
OOM-as-implicit-escalation as the fallback, not the primary path.

### Why three prompts, not one

- **Different jobs, different context budgets.** The orchestrator
  needs the GitHub state across all in-flight work but no source
  code. The execution agent needs the source code and the issue
  but no other in-flight work. The reviewer needs the PR diff and
  the issue but no planning history. Each prompt is shorter and
  more focused than a single combined prompt would be.
- **Independent failure modes.** An execution that OOMs or
  rate-limits doesn't take down the orchestrator. The next
  scheduled tick sees the stale lock (or the dead pod) and reaps
  it.
- **Conflict-of-interest separation for review.** The reviewer
  agent has fresh context, untainted by the planning that produced
  the PR. It's a real second-opinion pattern, not a self-review
  rubber stamp.
- **Parallelism on K8s.** Each execution and reviewer is its own
  Job/pod with its own resource limits and its own log stream.
  The orchestrator passes stay small and fast.

### Why three CLIs, not one

The orchestrator chooses a backend per task: `agent=claude`,
`agent=opencode`, or `agent=gemini`. The runner binary invokes
whichever was chosen, passing the model specified in the same
action (`model=opus`, `model=opencode/big-pickle`,
`model=gemini-2.5-pro`, etc.).

Routing to multiple backends pays off because each has a
different cost/capability curve:

- **Claude Code** is the strongest at complex refactors and
  design work, and its tool loop is the best-tested of the
  three. It's also the most expensive. Default for execution
  work that touches multiple files or needs real reasoning.
- **OpenCode** unlocks OpenAI models (GPT-5.x, codex variants)
  and free tiers (BigPickle, Nemotron, etc.) that are
  capable-enough for a lot of leaf work at zero or
  near-zero cost. Good fit for small scoped changes, doc
  updates, test scaffolding, reviewer passes on simple PRs.
- **Gemini** has Google Search built in. Dispatching to Gemini
  is what the orchestrator does when a task needs to verify
  an API surface, look up a library version, or cross-check
  an error message against upstream issues.

The runner keeps the backends interchangeable by passing prompts
verbatim — the execution and reviewer prompts describe goals
and file paths rather than any specific tool-call syntax, so
they work on whichever CLI got dispatched. This portability is
a constraint on how the prompts are written, not a runtime
feature: if a prompt starts reaching for Claude-specific tools,
it has to be reworked for portability or the orchestrator has
to be prevented from routing that task to non-Claude backends.

The policy that decides which backend for which task is driven
by issue labels plus the per-project `.hubert/README.md` (see
[`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md) for the env-var
contract the runner consumes). The orchestrator prompt owns the
policy; the image guarantees the CLIs are present; the runner
plumbs the choice through.

### Why GitHub-as-state, with no local persistence

The two-plane split makes this constraint load-bearing: the
control plane and execution plane share no filesystem, no
database, no memory. The only substrate both can read and write is
GitHub itself.

But even absent that constraint, GitHub-as-state has properties
the alternatives don't:

- The state is durable across binary reinstalls, cluster moves,
  and full backup loss.
- The state is human-inspectable in the GitHub UI without any
  tooling.
- The state is the same thing the user already looks at when
  thinking about the project — there's no second source of truth
  to keep in sync.
- The same locking mechanism (assignment + heartbeat comment)
  works identically whether two ticks are racing on the same
  runner or on opposite sides of the planet.

The cost is that every state read is a GitHub API call. At the
scheduled tick rate (~720/day/repo at 2-minute intervals) against
a 5k/hr authenticated rate limit, we're well inside the budget.
Webhook-triggered workflows don't add to the poll load — they
*replace* polling for reactive events.

### Why GitHub locking via assignment + heartbeat

Locking is the part of stateless distributed systems that's
usually hard. Here it's free because GitHub already provides:

- **Atomic assignment.** A `POST /repos/:owner/:repo/issues/:n/assignees`
  call is the lock acquisition. Two execution Jobs racing to
  start the same issue don't actually race — whichever one hits
  GitHub first wins, the other one's `gh api` call sees the
  assignment already in place and exits cleanly.
- **Heartbeat as liveness.** The execution agent updates an
  in-progress comment every few minutes. Stale heartbeat → dead
  execution → reapable. The orchestrator also has `kubectl get
  job` as a second liveness signal for Jobs it submitted.
- **Comment trail as audit log.** Every lock event is a comment,
  visible to humans, queryable via the API, free of charge.

The reaping step on each scheduled tick is the analogue of "the
lease expired, garbage collect" in a traditional locking service.
It's just that the lease expiration is tracked in comment
timestamps and pod status rather than in a Redis key.

### Why GitHub-account trust at origination, not at merge

The original brief required a human-in-the-loop checkpoint and the
obvious place to put it was at merge. After working through what
"human in the loop" actually means, the load-bearing property is
"a human Hubert trusts initiated this work." That's an
*origination* property, not a *merge* property. If Evan opens an
issue, the entire downstream chain (plan → implement → review →
merge) is trust-rooted in his original action. If a stranger opens
an issue, no amount of Hubert review at the end can change the
fact that the work originated with someone Hubert shouldn't trust.

So the trust gate moves to origination: Hubert acts iff the issue
author has commit access to the target repo, OR is `hubert-bot`
itself (which only got commit access because Evan gave it to
`hubert-bot`). Everyone else is silently ignored — no comment, no
label, no token spend, nothing for an attacker to interact with.

This makes Hubert a *trust amplifier*: it inherits the trust
posture of the underlying GitHub repo and does nothing to add or
subtract from it. See `SECURITY.md` for what that implies for the
threat model.

### Why orchestrator/reviewer can merge their own work

Follows from the trust-at-origination model. The reviewer is not
"approving an arbitrary diff" — it's approving a diff produced by
a chain of Hubert-internal agents acting on behalf of an
originally-trusted human. The conflict of interest in self-merge
is real for agents that *originate* work, not for agents that
*implement* trusted requests.

The reviewer is also a separate process from the executor with
fresh context, which is the structural defense against a sloppy
self-review. It's a different agent looking at the diff with no
memory of what the executor was trying to do — closer to a code
review than a self-review.

### Why dedicated `hubert-bot` account, PAT for the execution plane

- A dedicated account means commits and comments are clearly
  attributed to Hubert and not to Evan. When something goes
  wrong, the audit trail is unambiguous about who did what.
- Inside GHA workflows, the ambient `GITHUB_TOKEN` handles most
  control-plane operations (reading state, adding labels, posting
  comments). No PAT needed for those.
- The execution plane runs outside GHA, so K8s Jobs authenticate
  to GitHub with a PAT stored as a K8s Secret mounted into the
  Job. This is also what lets executor pushes trigger new GHA
  workflow runs — pushes authored by `GITHUB_TOKEN` are blocked
  from triggering downstream workflows by default (anti-loop),
  but PAT-authored pushes do trigger them, which is exactly what
  we want.
- A GitHub App is the cleaner long-term answer (per-repo
  installation, fine-grained permissions, no user-token expiry)
  and is the v2 upgrade path.

### Why Hubert ships binaries, not an image

Hubert upstream ships **executables and a contract**, not a
container image. Each deployment provides its own image that
satisfies the contract — see [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md)
for the full interface.

Separating like this falls out of the observation that the image
is the deployment-specific layer. Which base image, which LLM
CLIs, which credentials, which registry — all of those are
operator choices, not upstream defaults. Shipping an image
upstream would either force choices on operators or bloat the
image with everything every operator might want.

The contract is small: the image must have `hubert-runner`,
`git`, `gh`, and at least one supported LLM CLI on `$PATH`;
the Job gets a documented set of env vars and a writable
scratch mount; the runner reads its prompts from embedded
strings, so no prompt-file mount is needed. Everything else —
base image, credential mounting, registry, how the runner was
built into the image — is up to the deployment.

For the reference deployment this project targets (Evan's
`ekdromos` cluster via hermetic), the image bundles all three
CLIs (`claude`, `opencode`, `gemini`) and rides hermetic's
admission policy. See [`HERMETIC-HANDOFF.md`](HERMETIC-HANDOFF.md)
for the port inventory and [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md)
for the interface a different deployment would need to
satisfy.

### Why Go for what little code we write

Three binaries, all Go:

- **Runner** (`cmd/hubert-runner`). Runs inside each execution
  or reviewer Job. Clones the repo into the Job's scratch
  volume, acquires the GitHub lock (assignment + heartbeat
  comment), invokes the orchestrator-selected LLM CLI
  (`claude`, `opencode`, or `gemini`) with the embedded
  execution or reviewer prompt, enforces the per-run budget,
  pushes a branch, posts the outcome as a structured comment,
  and exits. On escalation, commits what it has, posts a
  `need-tier: …` comment, and exits cleanly. This is the
  binary downstream images must contain.
- **Dispatcher** (`cmd/hubert-dispatch`). A small tool the GHA
  control-plane workflows call to submit execution/reviewer
  Jobs. Renders a `batch/v1` Job + `v1` ConfigMap from an
  embedded `text/template`, applies via `kubectl`. Ported from
  `../hermetic/docker/tools/hermes-delegate/k8s.go` with the
  GCS Fuse mount block dropped.
- **Snapshot helper** (`cmd/hubert-snap`). Called from the
  orchestrator workflow to build the per-repo JSON snapshot
  (open issues, open PRs, recent `🤖 hubert-…` comments,
  collaborator list, kill-switch state) that the orchestrator
  prompt consumes. Go wins here because the snapshot schema
  wants real types; `gh` + `jq` got unwieldy in the first
  sketch.

Go wins on all three:

- Matches Evan's primary backend stack.
- `go-github` is mature.
- The hermetic submission logic is already Go; porting beats
  rewriting.
- Single static binaries; the runner binary is the only one
  that ships in a downstream image, and it's small.

### Why per-repo config in `.hubert/README.md`, not YAML

For v1, the config has at most five knobs: build/test/lint
commands, per-issue budget cap, optional model preference, and
free-form project notes for the orchestrator to read as context.
That's a markdown file with headings, not a structured document.
A loosely-parsed `.hubert/README.md` is friendlier to humans (it's
also a place to write contributor guidance) and friendlier to the
orchestrator (which reads it as raw text anyway).

The trusted-user list is *not* a config knob — it's derived from
the repo's actual GitHub collaborators. Using commit access as the
trust signal is the whole point.

When a second non-textual knob shows up that's hard to express as
prose, we graduate to YAML. Until then, prose is the right shape.

### Why ephemeral working trees, not a persistent clone

Each execution Job gets a fresh clone into its pod's emptyDir
volume. The tree dies with the pod. There's nothing to clean up
between runs and there's no shared state to corrupt — which
directly fixes the shared-working-tree race condition that Evan
hit in restaurhaunt.

On the hermetic cluster this is the default: the admission policy
enforces emptyDir with an 8Gi ceiling, and no Job shares storage
with any other Job.

### Why CI is the primary quality layer

The reviewer agent should read CI results, not just the diff.
Tests passing, lints passing, type checks passing — each is
*deterministic evidence* of correctness that an LLM doesn't have
to generate. This confines the reviewer's judgment to "is this the
right change?", which is where LLM review is actually strong, and
away from "does the code even work?", which deterministic tooling
answers better and cheaper.

The pattern that falls out:

- **Every PR gets a minimum CI pass before the reviewer agent
  runs.** Lint, fast unit tests, type check. Cheap per run;
  catches the obvious-wrong cases before spending review tokens.
  A red check here is a stop-the-line signal — the reviewer
  shouldn't even open the diff until the cheap gates are green.
- **Expensive stages gate on `ready_for_review`.** The GitHub
  event `pull_request: ready_for_review` fires exactly when a
  draft becomes non-draft, which is the right trigger for
  integration suites, perf benchmarks, and deep security scans.
  Pair every such workflow with a `concurrency` group keyed on
  PR number + `cancel-in-progress`, so pushing a new commit
  during review kills the in-progress run instead of double-
  billing minutes.
- **Agent-requestable stages use labels, not slash commands.** An
  agent deciding "this PR needs a perf check" sets label
  `needs-perf-test`; the workflow triggers on
  `pull_request: types: [labeled]`. Labels are visible in the PR
  UI as persistent state; slash-command comments are not. Labels
  can be added and removed as the review progresses and are
  trivially queryable via the GitHub API for orchestrator
  snapshots. The reviewer agent's "I want X checked" action
  becomes an idempotent label write, not a comment post.
- **Reviewer agent reads the CI status check list as part of its
  context.** A green check is not a rubber stamp — the reviewer
  still judges scope fidelity and design — but the reviewer
  treats the deterministic layer as ground truth for "does it
  work."

The scope discipline that matters: bake the *hooks* for all of
this into v1 (reviewer reads status checks; workflows use
concurrency groups; agents use labels not comments) but ship only
*one real stage* — lint + fast tests. Add stages only when a real
bug slips through the current set. It is very easy to dream up a
12-stage review pipeline that never ships.

Agent-requested expensive workflows need a budget gate, not free
rein. Repeated "run the full integration suite" requests can rack
up CI minutes fast. Enforce a per-PR cap — either the orchestrator
refuses to add the label past a threshold, or the label-triggered
workflow checks a budget state (commit count, prior run count, a
sentinel comment) before running.

## What's deferred to v2 / v3

- **GitHub App** instead of a PAT for the execution plane.
- **Attachment classifier** for cases where the injection vector
  is a file, not the issue text — relevant for the pidifool PDF
  flow.
- **"Lift" mechanism** for promoting an untrusted-author issue to
  actionable status via a committer's comment. The pidifool
  open-source flow needs this.
- **Cross-repo audit log** in a dedicated `hubert-log` repo or a
  GitHub Discussion thread. v1 relies on the per-repo comment
  trail.
- **Multi-stage CI pipeline.** v1 ships one stage (lint + fast
  tests); v2/v3 adds perf benchmarks, integration suites,
  security scans, and agent-requestable stages gated by labels
  and per-PR budget.
- **`actions-runner-controller`** as the control plane if
  GHA-hosted free-tier minutes ever become constraining.
