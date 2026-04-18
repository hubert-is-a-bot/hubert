# Feedback on PLAN.md

External review of PLAN.md at roughly the 3111-line state. Reviewer had no prior context on Hubert and read the document cold.

Document author should take this as input, not directive. Some items will be wrong; some will collide with context the reviewer didn't have.

## Framing from the operator

The operator wants PLAN.md to stay a **long-term living plan**, not a pitch or an execution contract. The later sections are deliberately aspirational — a way to not lose ideas that will be revisited once the basic loop is proven. Losing them is worse than keeping them speculative.

Therefore: the feedback below is **not** a request to delete aspirational content. It's a request to:

1. Make the confidence-gradient across the document architecturally visible, so readers (human or agent) never mistake *Someday* for *Now*.
2. Actually shrink what *Now* commits to, because right now *Now* is too big and its weight makes the aspirational parts look like commitments.
3. **Grow, not shrink, the later tiers** — capture directions before they're forgotten. Reviewer's additions are below.

## Overall assessment

Architecture is defensible. The two-plane split (GHA control + K8s execution), GitHub-as-state, checkpoint-and-exit escalation, trust-at-origination, and the two-bot-identity split are all sound choices that pay for themselves. Not bonkers.

The first-mile scope, however, is oversized for a research-phase project with no code yet, and a handful of specific v1 mechanisms have real problems worth addressing before implementation starts.

---

## 1. Structural change: horizon tiers

Add a top-of-document preamble that names confidence tiers and apply them as visible tags on every section header. Proposed tiers:

- **[Now]** — next implementation increment. Concrete, scoped, intended to execute against.
- **[Soon]** — earned by *Now* landing. Design is firm but subject to revision based on *Now*-learnings.
- **[Later]** — directions committed to, not yet designed in detail.
- **[Someday]** — vision sketches. Expect significant change on contact.

Every section header carries its tier: `## 6. v0 implementation plan [Now]`, `## 8. v2 roadmap [Later]`, `## 10.3 Sink destination [Soon]`, etc. The reader never has to guess.

Secondary benefit: *Someday* becomes a safe place to expand without readers mistaking it for commitment. Growth of aspirational sections is encouraged, not a risk.

## 2. Re-tiering the current sections

Current §6 is labeled v1 but contains items whose value only materializes *after* the basic loop exists. Reviewer's proposed re-tiering:

### [Now] — the actual first increment

- Project skeleton (§6 task 1).
- Runner binary — minimal. Single backend (Claude), no CLI adapter abstraction yet.
- Dispatcher binary (§6 task 3).
- Snap binary (§6 task 4) — without `recent_action_hashes`, without provider-health read.
- One GHA workflow template (§6 task 5), single-stage CI.
- Per-repo config (§6 task 6).
- One kill-switch granularity: a single repo-level `hubert-stop` label. Orchestrator short-circuits on presence.
- Two-bot identity split (§6 task 11) **only if** branch protection blocks the loop without it. Otherwise defer.
- **Human reviews and merges.** No auto-merge in *Now*. Reviewer agent does not exist yet — or if it does, it posts review comments only and never merges.
- End-to-end test against a throwaway repo (scope-reduced §6 task 15): file an issue, watch Hubert open a PR, click merge yourself.

### [Soon] — v1.5, earned by the loop working end-to-end

- CLI adapter abstraction (§6 task 10) — add second backend.
- Recovery pivot table (§6 task 8).
- Action idempotency (§6 task 12) — with the race-condition fix noted in §3 below.
- Three-tier kill switch (§6 task 7) — expand from single.
- Decision records + `hubert-log` sink (§6 task 13).
- Provider-health blob (§6 task 14).
- Reviewer agent with auto-merge, **gated by the human-in-the-loop gradient** described in the additions section.
- Structured logging (§6 task 9).
- Shadow-merge graduation — but see §3 below, the 2-week criterion is under-powered and needs rethinking.

### [Later] — v2

Everything currently in §8 v2 and the §10 open threads. Keep as-is; retag as `[Later]`.

### [Someday] — v3 and beyond

§8 v3 open-source bot, persona marketplace, per-role GitHub Apps, central scheduler with reaction primitive, `agency-agents` integration.

### Separate from the tier system

§11 "Building v1: the multi-agent work plan" is **meta-plan, not product**. It describes how the implementation is driven, not what Hubert is. Pull it into an appendix so readers don't mistake it for another product tier.

---

## 3. Technical concerns worth addressing before *Now* starts

### 3.1 Action idempotency has a race

§6 task 12 specifies: workflow reads `recent_action_hashes` from snapshot, checks for duplicate, executes, posts `🤖 hubert-action-hash` comment. Two concurrent workflows can both read the snapshot before either posts, both miss the dup, both execute.

GitHub comments don't provide CAS. Assignment-as-lock works for per-issue coordination because `POST /assignees` is atomic. Hash-comments are not.

**Suggested fix (still [Soon]):** move idempotency onto something atomic. Options:
- Use a label (`hubert-action-<short-hash>`) — label application is close to atomic per-issue and `GET /labels` is cheap.
- Tolerate the race: accept that sequential-dup is the common case (webhook + schedule fire on the same event) and design assuming parallel-dup occasionally slips through. The per-issue assignment lock catches the `dispatch-execution` parallel-dup case; only `comment`/`label` inline actions are at risk, and those are individually cheap to re-do.
- Drop hash-based idempotency entirely and rely on per-action target locks (issue assignment, PR `hubert-in-review` label).

### 3.2 "Reviewer replaces human after 2 weeks shadow mode" is under-powered

The current plan (§6 task 15 "Shadow-merge-gate graduation") is 2 weeks on a throwaway `corral` repo with trivial seed issues. After that, auto-merge is live on real repos.

Problems:
- 2 weeks on near-empty test content tells you nothing about the tail risk on real codebases.
- "No false approvals" is unmeasurable when nothing substantive is being approved.
- LLMs with shared training data share blind spots. "Different backend" review helps with per-PR blind spots, not with systemic ones (e.g., both models miss the same class of race condition).
- "Reviewer proposes failing test" catches bugs the reviewer remembered to test for, i.e., not the interesting class.

**Suggested revisions:**
- Make shadow mode the default steady state for trust-sensitive paths (see human-in-the-loop gradient below), not a time-boxed gate.
- Replace the 2-week time box with a **volume + diversity criterion**: N merges across M repos touching P distinct code areas with zero human-identified regressions, before auto-merge for that path class.
- Add the issue-derived test harness (below) — the single biggest mitigation for shared-training-data blind spots.

### 3.3 OOM/deadline post-mortem is harder than §10.1 admits

Incremental record emission helps, but a pod OOMkilled mid-`git push` may have committed nothing, pushed nothing, and its last useful heartbeat may be minutes stale. The orchestrator's recovery pivot table assumes escalation reasons are reliably legible; for involuntary kills they often aren't.

**Suggested additions to §10.1:**
- The cleanup Job needs to inspect `kubectl get events` for OOMKilled markers and the pod's last readable state (if any), not just assume "Failed == OOM."
- For `activeDeadlineSeconds` expiry specifically: the runner has advance warning (it knows the deadline). Add a "T-minus" self-check that forces a checkpoint-and-commit at deadline - 60s regardless of whether the LLM has returned.

### 3.4 Backend-portable prompts is a constraint with no enforcement

The plan asserts prompts are written backend-agnostically and warns that Claude-specific tool reaches "have to be reworked." There's no mechanism to detect drift. Claude/OpenCode/Gemini differ meaningfully in tool-use discipline, edit semantics, and self-correction patterns.

**Suggested [Soon] addition:** a prompt-portability test that runs each prompt against each declared backend on a fixture repo, with a structured pass/fail on each. Make drift visible as CI output rather than subtly-wrong production behavior.

### 3.5 Snapshot size on active repos

Current snapshot spec (all open issues + 5 comments each + all open PRs + 60min hashes + collaborators) scales linearly with repo activity and is fed to the orchestrator every tick. For a repo with 50 open PRs this is substantial tokens per pass, multiplied by tick rate.

**Suggested [Soon]:** a snapshot-diet mode — the orchestrator pass gets a filtered view (kill-switch state, new/changed items since last tick, reserved hash window) rather than the full state. Full state on first tick of the day and on explicit signals; diff-mode otherwise.

### 3.6 Trust escalation from "write access" to "cluster RCE on shared bill"

A committer going from "can propose a change that humans review" to "can drive arbitrary code execution on the cluster against the shared Anthropic bill" is a meaningful trust escalation. The plan correctly calls compromised-committer out of scope and documents that Hubert inherits GitHub trust, but it understates what a *legitimately-granted* write-access collaborator can now do via issue bodies.

**Suggested documentation addition (not design):** in §4 Trust and security, an explicit section on "what write access means under Hubert" so operators correctly price collaborator-list hygiene, 2FA requirements, and branch-protection posture before adopting.

### 3.7 Cost cap soft by one tool call

§4 threat 3 acknowledges a single adversarial tool call can exceed the cap. A recursive `find /`, a large download, a runaway `go build -v` all fit. Not fatal, but:

**Suggested [Soon]:** per-tool-call timeout and egress cap at the runner level (wrap Bash invocations, not the LLM call). Cheap, addresses the unbounded-single-call class.

### 3.8 `hubert-is-a-bot/hubert-config#1` chicken-and-egg

Global kill switch references a repo/issue that doesn't exist at *Now* start. Plan doesn't specify who creates it, when, or how its permissions are bootstrapped. Needs to be in the prerequisites block, not assumed.

### 3.9 `hubert-log` as synchronous sink, failure modes undefined

Every phase-transition writes to `hubert-log` via `gh api PUT`. What happens when the log repo is unreachable — runner fails, skips emission, degrades to in-memory buffer that flushes on recovery? The plan says "emission happens" but not "what happens when emission can't."

Default should be **skip emission, continue the run, log the skip to stderr**. Decision records are valuable but not worth blocking execution on.

### 3.10 PAT rotation has no automation

§4 threat 6 says PATs are "rotated on a schedule." No mechanism in v1. A leaked PAT lives until an operator notices.

**Suggested [Soon]:** a rotation helper script (idempotent, like the existing hermetic setup scripts) + documented expiry monitoring. Real rotation automation is [Later], but the helper + monitoring is cheap now.

---

## 4. Additions to the aspirational tiers

The operator explicitly wants these tiers to **grow**, not shrink. Reviewer suggestions — most useful as [Later] or [Someday] seeds:

### 4.1 Issue-derived test harness as a first-class agent

A separate agent that sees **only the issue body** (never the diff, never the executor's reasoning) and proposes the tests that must pass for the issue to be considered resolved. Executor implements; reviewer runs executor's code against the test-deriver's tests.

This closes the shared-training-data hole better than "reviewer proposes failing test" because the test-deriver never saw the proposed solution. It's the closest LLM-era analogue to TDD written by a separate engineer.

Tier: [Later], or [Soon] if it turns out to be the primary answer to §3.2 above.

### 4.2 Reviewer-as-adversary framing

Prompt the reviewer not "does this look right" but "find the worst input that breaks this." Different cognitive framing, different failure-mode coverage, cheap to add. Likely composable with the issue-derived test harness (adversary outputs proposed breaking inputs; those become tests).

Tier: [Soon].

### 4.3 Partial-credit merge outcomes

Reviewer can merge approved portions of a PR and reopen unapproved portions as follow-up issues with `decomposition-depth: parent+1`. Gives scope-splitting as a reviewer output, which is the natural response to "90% of this is fine, this one function is wrong." Currently the only options are "merge" and "iterate" — partial-credit is a missing middle.

Tier: [Later]. Composes with the iteration cap and with personas.

### 4.4 Replay-based regression harness

Every successfully merged Hubert PR becomes a fixture: (issue-body, repo-state-before, merged-diff, test-outcomes). Changes to orchestrator prompts, reviewer prompts, or routing policy get regression-tested against the fixture corpus deterministically.

This is the single missing thing for iterating on prompts without fear — today a prompt tweak is a guess, because the only validation surface is "run it on a new issue and see." With a fixture corpus, prompt changes have a regression signal.

Tier: [Later]. Natural fit with the decision-records sink.

### 4.5 Post-merge observation window

For N days after a Hubert merge, the orchestrator watches for:
- The merge getting reverted.
- A follow-up issue citing the merged PR as the source of a new problem.
- A later commit adding a test that would have failed pre-merge.
- A human `@hubert-is-a-bot` comment flagging a regression.

Each signal feeds into the decision-record store and reviewer calibration. This is the **only ground-truth signal** for "was the reviewer right?" — everything else is grading LLM output with LLM output.

Tier: [Later]. Arguably the highest-value *Later* addition because it's the feedback loop that makes Hubert improvable from normal project workflow.

### 4.6 Human-in-the-loop as a gradient, not a switch

Replace the binary shadow-mode → auto-merge graduation with a policy surface:
- Auto-merge permitted: scoped, isolated, fully-tested changes on non-sensitive paths.
- Shadow mode (reviewer approves; human merges): anything touching trust-sensitive paths (auth, secrets, schema, public API, dependency updates, CI config).
- Human required (Hubert refuses to dispatch): paths the project declares off-limits.

Driven by path globs in `.hubert/README.md` and/or labels. Allows real-codebase adoption without an all-or-nothing trust jump.

Tier: [Soon]. This is the single architectural change reviewer believes is worth promoting *out* of [Later] into [Soon] — it's what makes auto-merge safe enough to ship at all.

### 4.7 Capability negotiation instead of up-front tier prediction

Today the orchestrator *predicts* tier/model/agent; the executor confirms or escalates. Make "insufficient-capability: need X" a first-class response type so the executor *observes* rather than the orchestrator guesses.

Already half-present in checkpoint-and-exit escalation. Promote it from "failure recovery path" to "normal control flow." Reduces up-front tier-over-provisioning (cost) and up-front tier-under-provisioning (escalation churn).

Tier: [Soon]. Small change to action schema; bigger change to orchestrator prompt.

### 4.8 Cross-repo pattern extraction from decision records

Once decision records accumulate across repos, patterns surface: "this executor consistently mishandles Go generics," "schema-migrator persona never writes the down migration when the issue title contains 'drop'," "Gemini dispatches on issues with attachments fail at 3× the rate."

Emerges from the log aggregator; feeds back into persona tuning and orchestrator routing policy.

Tier: [Later]. Composes with the replay harness and the post-merge observation window.

### 4.9 Structural review diversification

Instead of one generalist reviewer, parallel specialized reviews: security-review, scope-review, test-coverage-review, data-shape-review. Each is focused prompt + small context (cheap). Aggregation is a meta-pass.

Different failure modes caught by different prompt shapes, not just different model weights. Composes directly with personas — a security-auditor persona is the security-review agent.

Tier: [Later]. Probably the cleanest way to operationalize personas on the review side.

### 4.10 Confidence-weighted approval

Reviewer reports confidence alongside approve/reject. Low-confidence approvals escalate to human regardless of auto-merge policy. Confidence is calibrated over time via the post-merge observation window (§4.5) — confidence that correlates with post-merge incident rate is kept; confidence that doesn't is discarded.

Tier: [Later].

### 4.11 Issue-author feedback loop

When a trusted committer reacts negatively to a merged Hubert PR (revert, follow-up issue, explicit `@hubert-is-a-bot this was wrong` comment), that signal is a first-class input to reviewer calibration. Makes Hubert improvable from **normal project workflow** — no special annotation required, just the project's existing signals.

Tier: [Later]. Foundational for the reviewer calibration story.

### 4.12 Decision-record completeness as a graded substrate

Current §6 task 13 treats decision records as per-run artifacts. Higher value: treat the **completeness of the decision-record corpus** as a graded substrate. A PR merged with a decision record that omits "tests run with results" is lower-quality substrate than one with a full record. Over time, incomplete records get backfilled by later runs referencing the same issue.

Tier: [Someday]. Speculative but the direction matters for v2 aggregator design.

---

## 5. Hygiene items

- **Single home for "what v1 defers."** Currently interleaved in §6 "Out of scope," §8 v2 roadmap, and §10 open threads. Pick one canonical home and cross-reference from the others.
- **Status line update.** The `> Status: research-phase design. No code yet.` line should reference the tier system once it exists, so readers arriving at the top of the document know how to read.
- **§10 open threads — tier them individually.** Some (idempotency details, §10.5; PAT scopes, §10.7) are *Now* blockers; others (cross-repo budget pooling, §10.9) are *Later* threads. The flat list currently mixes urgency levels.
- **Prereq §6 "Prerequisites" block needs `hubert-is-a-bot/hubert-config` repo/issue bootstrap** (see §3.8 above).
- **§11 "multi-agent work plan" → Appendix A.** Meta-plan, not product tier.

---

## 6. What reviewer did not evaluate

- The three prompt files in `prompts/`. Reviewer read only PLAN.md.
- The reference deployment's actual state in `../hermetic`. Took the plan's description at face value.
- Whether the Go module layout in §6 task 1 is conventional for the project's stack. Didn't inspect neighboring repos.
- Whether `hubert-log`, `corral`, and the `hubert-*-bot` accounts exist yet. The plan lists them as prereqs; reviewer didn't verify.

Anything that depends on those should be verified against reality before acting on the feedback above.
