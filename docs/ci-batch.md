# Opting a repository into batch CI

Batch CI is optional. Without `.devctl/ci-batch.yaml`, generator output and
ordinary PR checks are unchanged. This file is repository-owned generator input,
read by `devctl gen workflows`, `devctl gen precommit` and `devctl gen circleci`.
Alignment preserves it; do not edit generated workflows or CircleCI output.
The generator does not change GitHub rulesets, CircleCI project settings or any
consumer repository. Consumer rollout and real-queue proof remain separate work.

## Prepare without reducing PR validation

Commit a policy such as this, replacing **all** result identities with those
observed from the repository's actual required checks:

```yaml
mode: prepare
baseBranch: main
gateAppID: 15368 # GitHub Actions App ID on github.com
results:
  - name: pre-commit
    appID: 15368
    workflow: .github/workflows/zz_generated.pre-commit.yaml
  - name: "ci/circleci: build"
    creatorID: 123 # example only: use the observed trusted CircleCI status creator ID
```

List every required result, including repository-owned custom jobs and nested
reusable-workflow check names exactly as GitHub reports them. A check run uses
`appID`; a commit status uses `creatorID`, the numeric identity of the trusted
producer. Exactly one is required. For Actions, also supply `workflow`, the caller's
workflow file path, so a new queued suite invalidates an older successful suite.
Do not list the aggregate itself. A missing context is a failure to obtain proof,
not a reason to drop the context from this list.

Regenerate all three CI generators with the repository's existing arguments.
Preparation keeps full PR CI, including the PR title check, and adds unfiltered
`merge_group` triggers to generated validation workflows. This includes pre-commit,
chart tests, chart schema/documentation validation, Kyverno tests, gitleaks,
Actions analysis and README links. The README check fails on broken links on the
queue, while retaining its scheduled issue-reporting behavior. Release, main,
tag, dispatch and scheduled triggers and existing concurrency remain intact.
PR-specific automations are not moved to merge groups.

The generated `zz_generated.batch_ci.yaml` reports `batch-ci`: fast admission on
PRs, and a bounded wait for every configured result on the event's exact queue
SHA on merge groups. It requires successful completed checks, never skipped,
neutral, cancelled or failed checks. Missing, stale-SHA and foreign-producer
results cannot satisfy it. For Actions it verifies the latest merge-group
workflow suite and attempt, including a newer suite that has no check yet.
It reads GitHub's check/status APIs with a read-only token, without checking out
or running PR code. API failures fail the gate. The deadline is 45 minutes;
a slower pipeline must be investigated before adoption.

CircleCI setup verifies that checkout on `gh-readonly-queue/*` is exactly
`CIRCLE_SHA1`, then continues the same generated configuration merged with
`.circleci/custom.yml`. Single- and multi-image repositories use the same guard.
Custom jobs, dependency edges, filters and contexts remain repository-owned and
are not rewritten. Check custom filters explicitly: every required branch job
must admit queue branches and must not require an open PR; release-only jobs
must retain their tag filters. A required custom job filtered out of a queue
commit leaves the aggregate waiting and eventually red.

CircleCI must receive pushes for `gh-readonly-queue/*` and publish results on
that exact SHA. Its project-level “only build pull requests” setting and any
branch allowlists must permit queue branches. Do not simulate a queue pipeline
by building the current main branch or a PR head. No new trigger token is
introduced by this generator; use the existing CircleCI repository integration.
If that integration cannot build queue refs, remain in preparation mode.

The PR-only orb/trigger work in
https://github.com/giantswarm/architect-orb/issues/1049 and
https://github.com/giantswarm/github-workflows/issues/347 (generator work:
https://github.com/giantswarm/devctl/pull/2988) is related, not a prerequisite
already proven here. Do not enable its open-PR gating on queue jobs unless the
released shared implementation explicitly exempts queue refs. Shared reusable
workflows must also successfully validate `merge_group` with the exact checkout;
prove this for each listed context before transitioning. Fix missing shared
support in its owner and reference its public issue, never patch generated output.

## Prove, enforce, then disable duplicate PR work

1. Generate and merge preparation support while keeping the current full PR
   requirements. Review the complete result list against server requirements.
2. Use an authorized merge-queue pilot with these workflows and CircleCI settings.
   Record the real `merge_group` head ref and SHA, Actions run and check URLs,
   CircleCI pipeline revision, job URLs and trusted producer IDs. Confirm every
   required custom job, dependency and context ran. Prove that an intentionally
   unsuccessful required result prevents the aggregate from succeeding, and that
   a subsequent queue SHA cannot reuse the earlier SHA's successful result.
   An ordinary PR run or synthetic local fixture is not live evidence.
3. After that evidence is available, enforce the target branch's server-side
   merge queue and required `batch-ci` aggregate bound to the Actions App. Retain
   ordinary full PR requirements through this step. No direct-merge or bypass
   actor may evade queue validation. Confirm the effective branch rules and
   verify the target branch's real queue results before proceeding.
4. Only with that coordinated protection active, replace individual full-PR
   requirements with the matching aggregate, retain fast title/admission checks,
   change the policy to `mode: batch`, and regenerate all three generators.
   Protect policy/workflow changes through the same queue and review process.
   Never require only fast PR admission while direct merges remain possible.

In batch mode, generated expensive Actions workflows omit `pull_request` and
run on the exact queue commit instead. The title workflow stays on PRs. The
aggregate's PR admission and queue run both read effective server rules and fail
unless `merge_queue` and the app-bound required `batch-ci` result are present.
This check detects a missing policy; it does not grant permission to change it
or prove the absence of privileged bypass actors.

CircleCI batch setup halts ordinary PR pipelines before continuation. It does
so only with PR metadata and without a tag; queue, default branch, maintenance
`release-*` branches, and all tag builds continue. Missing PR metadata retains
the complete pipeline rather than guessing and suppressing validation. Verify
that the integration supplies this metadata before claiming duplicate CI savings.
Custom jobs merged into the continuation follow the same batch suppression.
Active queue/main/release work is not cancelled: existing Actions cancellation
remains PR-only. Queue batch sizing and release serialization remain the server's
and existing release workflows' responsibility.

This prerequisite does not authorize a live ruleset change or consumer rollout.
Those steps, their exact-commit evidence, and throughput measurement belong to
the consuming repository, for example https://github.com/giantswarm/beekeeper/issues/3300.

## Roll back in this order

1. Set `mode: prepare` and regenerate workflows, pre-commit and CircleCI setup.
   Merge through the still-protected queue. Confirm full ordinary PR checks run
   and succeed on a new PR head; preparation retains queue validation too.
2. Restore the ordinary app-bound full-check server requirements and verify
   effective protection, while retaining the queue requirement until then.
3. Only after ordinary full PR CI and requirements are restored, remove the
   aggregate requirement and enforced queue if desired. Remove the policy and
   regenerate to restore default generated output. Remove the generated aggregate
   workflow only after its server requirement has been removed.

The contract tests cover both transition directions, unchanged defaults,
regeneration, exact checkout, result identity, unsuccessful/missing results and
superseding workflow suites. They are synthetic CI coverage, not a substitute for
the consuming repository's real queue proof.
