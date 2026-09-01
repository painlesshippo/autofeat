# Autofeat Herdr workflow

This directory contains Autofeat's project-specific orchestration policy. It is
separate from Herdr's application configuration, which Herdr stores as
`config.toml` in the user's configuration directory.

The workflow is intentionally independent of model vendors and coding
harnesses. Before starting a change, set each participating role's
`runtime.kind` and optional runtime arguments in `config.yml`, or provide them
to the orchestrator in the task prompt. A `null` kind means the orchestrator
must ask the human; it must never guess.

For a code change, the orchestrator creates an isolated Git worktree and Herdr
workspace. The coder hands work to the reviewer, which rebases the branch onto
the current base and runs the project's required checks itself before reviewing
the complete diff, and a rejection returns to the coder. That loop continues
until review accepts the complete change. Acceptance is followed by the
orchestrator pushing the revision the reviewer accepted and opening a pull
request against the base branch, whose URL is the handoff to the human; human
review happens on that pull request, and nothing is merged or deleted
automatically.

Running the checks belongs to the reviewer rather than to a separate role: the
result is a deterministic pass or fail, so it needs an independent runner, not
an independent judgment. The coder still runs fast checks while developing,
but only the reviewer's own run gates acceptance.

## Starting and staying fresh

A workspace must start from the tip the remote actually has.
`create-workspace.sh` enforces that rather than leaving it to an instruction:
with no explicit base argument it fetches `project.base_branch` over HTTPS from
`project.push_url` into `refs/herdr/base/<base branch>` and branches from that
ref, so a stale local base branch cannot be inherited. The fetch writes only
into `refs/herdr/base/`, a namespace this workflow owns;
`refs/remotes/origin/` describes the `origin` remote and is left alone. A
failed fetch stops workspace creation with a message and creates nothing,
because silently falling back to the local branch is the bug the fetch exists
to prevent. Passing an explicit base argument skips the fetch, for the
deliberate case of branching from a chosen ref.

Fetching once at creation is not enough when tasks overlap: a branch created
from a fresh base goes stale while the work happens, and the base can move again
before review ends. So the reviewer refetches the base and rebases the task
branch onto it before running the checks, and its verdict names the post-rebase
commit. A rebase that conflicts is aborted and returned to the coder to
resolve, which keeps the reviewer's boundary intact: it may replay a change
onto a newer base, but it never alters what the change does. The result is that
acceptance means the checks passed on the change as it applies to the base as it
stood at review time, not to whatever the base was when the workspace was
created.

The branch is pushed to `project.push_url` rather than through the `origin`
remote, so the workflow does not depend on the human's Git transport or
credentials and never modifies them; the reviewer's fetch uses the same URL for
the same reason. Because the reviewer rebases, an accepted revision may not
descend from what an earlier round pushed, so the push uses
`--force-with-lease` qualified with the remote tip read just beforehand: it can
replace the branch's own earlier revision, but it is refused rather than
clobbering a branch someone else advanced. Set
`workflow.open_pull_request_automatically` to `false` to stop at a summary
instead, and `workflow.pull_request_draft` to `true` to open the pull request as
a draft.

Runtime handoff files belong under `.herdr/runtime/` inside the worktree and are
ignored by Git.

## Helpers

* `scripts/create-workspace.sh` fetches a fresh base and creates the task
  branch, worktree, and Herdr workspace.
* `scripts/start-agent.sh` starts a configured role in an existing pane and
  sends its instructions plus a context file.
* `scripts/handoff-agent.sh` sends updated role instructions and context to an
  existing agent.

Run `mise run herdr:check` to validate the YAML configuration and helper
scripts.
