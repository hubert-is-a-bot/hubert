# Hubert security model

> Status: research-phase design. The threat model and defenses
> here are intentionally narrower than the original brief, because
> the trust gate moved from the merge step to the origination step
> (see `ARCHITECTURE.md`). Most of the brief's elaborate prompt-
> injection defenses become unnecessary; what's left is sharper.

## TL;DR

- **Trust gate is at origination, not at merge.** Hubert acts iff
  the issue author has commit access to the target repo, or is
  `hubert-bot`. Everyone else is silently ignored.
- **Hubert is a trust amplifier.** It inherits the trust posture
  of the underlying GitHub repo and adds nothing. Defenses against
  credential theft, branch protection, and 2FA enforcement live at
  the GitHub layer, not in Hubert.
- **GitHub is the lock and the kill switch.** All coordination,
  including the global stop signal, is through GitHub state — so
  it works on stateless deployments and Evan can hit the stop
  switch from his phone.
- **Defenses against runaway costs and runaway recursion are
  baked into the orchestrator and execution prompts**, with the
  orchestrator workflow and the runner binary as sanity-check
  backstops.

## Threat model

### Primary threat

A non-committer triggers Hubert work on a public repository. This
is the failure mode the trust gate exists to prevent. The most
concrete instance is **pidifool**: anyone in the world can file an
issue against a public Go PDF parser, and we want Hubert to be
able to act on legitimate bug reports without acting on malicious
ones. The v1 answer is "Hubert ignores all non-committer issues
entirely." The v2 answer (the "lift" mechanism, see below) lets a
committer manually promote an issue to actionable status.

### Secondary threats

1. **Compromised committer account.** If an attacker takes over
   Evan's GitHub account, or any other committer's account, Hubert
   will happily act on whatever malicious issue they file.
   *Mitigation*: lives at the GitHub auth layer (2FA enforcement,
   hardware keys, branch protection rules). Hubert doesn't add
   independent defense here. **Document this explicitly so users
   don't assume Hubert is a defense-in-depth layer against
   credential theft.**

2. **Malicious attachment in a trusted-author issue.** Evan files
   a perfectly legitimate pidifool bug report and attaches the
   PDF that crashed the parser. The PDF itself is the injection
   vector — either against the parser (which is the bug we want
   to fix) or against the LLM that's reading the issue (which
   might extract instructions from PDF metadata or text content).
   *Mitigation v1*: execution agents that consume attachments
   should treat attachment content as data, not instructions. The
   execution prompt makes this explicit. *Mitigation v2*: an
   isolated classifier pass on attachment content before any
   tool-using agent sees it.

3. **Budget-of-spend DoS.** A confused execution agent gets stuck
   in a loop, runs Bash commands forever, or pulls a giant
   transitive dependency tree into its working tree. *Mitigation*:
   per-issue and per-repo cost caps, enforced by both the prompt
   and the runner binary. The runner will SIGTERM the LLM CLI
   subprocess when tracked spend exceeds the per-run cap.
   Kubernetes-side `activeDeadlineSeconds` on the Job is a
   second backstop against a runner that fails to self-limit.

4. **Execution-agent tool misuse.** The execution agent has Bash,
   Edit, and Write tools and runs inside a Kubernetes Job whose
   capabilities are set by the deployment's admission policy.
   A buggy or adversarial prompt could in principle have it
   touch files outside the working tree. *Mitigation v1*: each
   execution runs as its own K8s Job in its own pod with an
   `emptyDir` working volume; the pod has no access to any
   other Job's state, no shared PVC, no host mounts. The
   hermetic reference deployment's `ValidatingAdmissionPolicy`
   rejects `hostNetwork`, `hostPID`, and privileged containers
   at admission time. The execution prompt also explicitly
   forbids leaving the working tree; the admission policy is
   the structural backstop if the prompt doesn't hold.
   *Mitigation v2*: tightening the admission policy further
   (e.g., read-only root filesystem, egress NetworkPolicy) and
   adding per-project allowlists for which CLIs may run.

5. **Recursive plan-decomposition fork bomb.** Hubert can file
   sub-issues against itself, and Hubert acts on its own issues
   (because they pass the trust gate). A confused orchestrator
   could file an issue that leads to three sub-issues that each
   lead to three more, etc. *Mitigation*: each sub-issue carries
   a `decomposition-depth: N` tag in a structured comment, and
   Hubert refuses to act on issues at depth > 3 without
   escalation. The orchestrator prompt enforces this; the
   binary cross-checks it.

6. **Hub of trust failure.** `hubert-bot`'s PAT is itself a
   credential. If it leaks, an attacker can do anything Hubert
   can do. *Mitigation*: PAT scoped to only the watched repos,
   stored in the host's keyring (or as a Kubernetes Secret),
   rotated on a schedule, with an audit trail of recent
   `hubert-bot` actions visible in the GitHub UI. The v2 GitHub
   App migration replaces this with installation tokens that
   are short-lived and per-repo by construction.

### Out of scope for v1

- **Attachment-content classifier.** Documented gap. v2.
- **The "lift" mechanism for promoting public-user bug reports.**
  Documented gap. v2-or-v3 depending on when the pidifool flow
  becomes load-bearing.
- **Defense against a compromised committer account.** Lives at
  the GitHub layer; Hubert is explicit about not adding defense
  in depth here.
- **Defense against attacks on Anthropic, OpenAI, Google, or
  GitHub themselves.** Out of scope.
- **Defense against supply-chain attacks on the LLM CLIs
  (`claude`, `opencode`, `gemini`), on Hubert's own
  dependencies, or on the deployment's container image.** Out
  of scope; standard module hygiene and image pinning apply.
- **Defense against a compromised deployment image.** The
  operator owns the image per
  [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md); if the image is
  malicious, Hubert inherits that. Pin image digests, review
  the Dockerfile.

## Trust gate, in detail

The trust gate is binary and lives in the orchestrator prompt
(with sanity backstops in the orchestrator workflow, which
filters untrusted-author events before they ever hit the
orchestrator prompt, and in the runner, which re-checks before
doing any expensive work):

```
trusted(issue) :=
    issue.author == "hubert-bot"
    OR issue.author has commit/admin access to issue.repository
```

The collaborator list is queried per tick via
`gh api repos/:owner/:repo/collaborators` (cached briefly to
avoid rate limit pressure). Anything not in that set is
ignored — silently. No comment, no label, no token spend, no
acknowledgement of any kind. **Silence is the cheapest and
least-attackable response to an untrusted issue.** An attacker
who files an issue and gets no response can't even confirm that
Hubert is watching the repo.

There is intentionally no "warn the author politely" path in v1.
That path is a token-spend vector and an information leak. The
v2 "lift" mechanism is a controlled way to bring untrusted
issues into the actionable set, but only via an explicit
committer action.

## Layered defenses around trusted-but-risky action

Even within the trusted set, Hubert applies budget and sanity
limits so that a buggy issue or a confused agent can't do
unbounded damage:

1. **Per-issue cost cap.** Default $5 (configurable per repo).
   Enforced by the execution prompt (which is told its budget
   and instructed to escalate before exceeding it) and by the
   runner binary (which SIGTERMs the LLM CLI subprocess when
   tracked spend exceeds the cap).

2. **Per-day per-repo cost cap.** Default $50 (configurable).
   `hubert-snap` sums `🤖 hubert-cost` comments into the
   per-repo `daily_spend` field of the orchestrator snapshot;
   the orchestrator prompt refuses to emit `dispatch-*`
   actions past the cap, and the orchestrator workflow
   cross-checks the parsed action list before dispatching.
   Resets at UTC midnight.

3. **Iteration cap on the implementer feedback loop.** After 3
   rounds of "PR rejected, fix it" without convergence, Hubert
   labels the issue `hubert-stuck`, posts a summary of what was
   tried, and waits for human intervention. Prevents loops where
   the executor and reviewer can't agree.

4. **Stale-lock reaping.** An execution agent that crashes,
   OOMs, or rate-limits leaves its assignment + heartbeat
   comment behind. The next orchestrator tick notices the stale
   heartbeat (>30 min old, no associated open PR) and reaps it:
   posts a `🤖 reaping stale run X` comment, unassigns, and the
   issue becomes eligible to dispatch again. Handles all crash
   modes uniformly.

5. **Global kill switch via GitHub.** A designated control issue
   in the central `hubert-bot/hubert` repo (or a per-repo
   `.hubert-stop` issue, for finer-grained control). If the
   issue is open and labeled `STOP`, the orchestrator workflow
   exits at the top before invoking any LLM CLI, and the runner
   binary re-checks on Job startup to catch anything queued
   before the flip. **The kill switch is GitHub-based, not
   local-file-based**, so it works across both planes (GHA and
   K8s) and so Evan can hit it from his phone.

6. **Ephemeral working trees.** Each execution agent runs in its
   own fresh working tree with no carryover from previous runs.
   No shared state to corrupt, no leftover files from a previous
   crashed run, no possibility of one execution stomping
   another.

7. **`hubert-bot` PAT scoping.** PAT is scoped to only the
   watched repos. Even if the PAT leaks, the blast radius is
   bounded to repos the user already explicitly chose to expose
   to Hubert.

8. **Recursive decomposition depth limit.** Each sub-issue Hubert
   files for itself carries a `decomposition-depth` tag in a
   structured comment. Hubert refuses to act on depth > 3
   without escalation.

## Crash and recovery

The concrete failure mode that motivates the recovery design is
**the cycle-4 rate-limit incident from the day this design was
written**: an execution agent in a long restaurhaunt session hit
an Anthropic rate limit mid-flight, the orchestrating Claude
Code session noticed only by accident, and the user had to
manually salvage state from a partially-written branch.

The Hubert design avoids that failure mode by construction:

- The execution agent's last action before any large/slow
  operation is to update its heartbeat comment with what it's
  about to do. If it crashes mid-operation, the heartbeat trail
  is the recovery breadcrumb.
- A stale heartbeat triggers reaping by the next orchestrator
  tick. Reaping is non-destructive: it unassigns the issue and
  posts a summary, but does not touch the branch or the PR.
- The next time Hubert dispatches an execution agent for that
  issue, the new agent reads the issue thread (including the
  reaped run's heartbeat trail and the reaping comment) and
  decides whether to resume from where the previous run left
  off, start over, or escalate.
- The reviewer agent never trusts an execution agent's
  self-report of completeness; it independently verifies the
  PR against the issue body.

## What Hubert is NOT

Calling these out so users don't acquire false expectations:

- **Hubert is not an authentication system.** It is not a defense
  against compromised GitHub accounts.
- **Hubert is not a code review system in the traditional sense.**
  The reviewer agent is a sanity check on the executor agent,
  not a substitute for human review when humans should be
  reviewing (e.g., security-sensitive changes).
- **Hubert is not a sandbox on its own.** Execution agents run
  inside Kubernetes Jobs whose isolation posture is set by the
  deployment — the hermetic reference deployment enforces
  emptyDir-only storage, no host network, no privileged
  containers, and a namespaced RBAC scope. If you deploy to a
  cluster without equivalent admission-policy controls,
  Hubert does not add them on your behalf.
- **Hubert is not a multi-tenant service.** It is one user's
  personal automation, running on credentials that user owns.
  Don't share `hubert-bot` across users.
