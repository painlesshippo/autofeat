# Session Lifecycle

## Create Or Extend A Session

From inside the source repository:

```sh
autofeat new feature/name
```

Use an explicit local path or remote URL when the current directory is not the
intended source:

```sh
autofeat new feature/name --local /path/to/repository
autofeat new feature/name --remote https://github.com/example/repository.git
```

Running `new` again with the same feature name attaches another repository to
the existing session. Local sources become Git worktrees. Remote sources are
cloned into the session.

Use `--ref REF` to select a branch, tag, commit, full ref, or revision
expression as the base. Autofeat remembers an explicit base reference for that
repository. Repository creation does not fetch; an unqualified branch uses its
cached `origin` branch when available, then its local branch.

## Select Existing Sessions

`open`, `run`, `sync`, `status`, and `teardown` accept exact names, multiple
selectors, or wildcard selectors:

```sh
autofeat status feature/name
autofeat status feature/one feature/two
autofeat status "feature/*"
```

Always quote wildcard selectors. `status` defaults to all sessions when no
selector is provided. `remove` is different: it accepts one exact feature name
and one explicit repository source.

## Inspect And Enter Work

Use these offline inspection commands first when the target session is not
clear:

```sh
autofeat list
autofeat status feature/name
```

`list` shows active sessions and cached base drift. `status` reports branch,
worktree cleanliness, base drift, push relationship, and an actionable state.
Neither command fetches or modifies repositories.

Open the session workspace in the configured editor:

```sh
autofeat open feature/name
```

Open an interactive configured agent instead:

```sh
autofeat open feature/name --copilot
```

Start the configured headless agent, optionally appending an objective to the
session's `TASK.md`:

```sh
autofeat run feature/name
autofeat run feature/name --task "Implement the requested change and test it"
```

When `run` starts inside a Git repository, it first attaches that repository if
the session does not already contain it. Run from outside a Git repository when
the user intends only to start an existing session.

## Synchronize

Inspect the session before synchronization:

```sh
autofeat status feature/name
autofeat sync feature/name
```

Every repository must exist, be on the feature branch, have no staged,
unstaged, or untracked changes, and have no rebase in progress. Autofeat
validates every repository before fetching or rebasing any of them.

Origin branches are fetched before rebasing. Repositories without an `origin`
use their local base branch. Tags, commit SHAs, and other immutable references
are resolved locally and are not fetched.
