# autofeat
`autofeat` manages disposable Git worktrees for concurrent AI-agent feature
development. A feature session can group worktrees from multiple repositories
and open them together in one VS Code workspace.

## Requirements
* Go 1.26.5 or later, to build or install the CLI.
* Git installed and available on `PATH`.
* VS Code's `code` command on `PATH` to use the default workspace-opening
  command. Another editor command can be configured instead.
* Linux's `xdg-open` command on `PATH` to open generated reviews in a browser.
  In WSL, `autofeat` uses `explorer.exe` and `wslpath` instead.

On its first use, `autofeat` creates `$HOME/.autofeat/config.json`:

```json
{
  "workspace_base_dir": "/home/your-user/.autofeat-workspaces",
  "editor_cmd": "code",
  "headless_cmd": "copilot"
}
```

Set `workspace_base_dir` to choose where feature worktrees and
`.code-workspace` files are created. Set `editor_cmd` to the executable that
should receive a workspace-file path. Set `headless_cmd` to the interactive
agent command used by `run`.

`$HOME/.autofeat/state.json` stores `default_base_branch`, which defaults to
`master`. When a repository is added, `autofeat` detects `master` or `main`
and records the result as that repository's `base_branch`. Change either value
in the state file to use a different persistent review base; a command-line
`--base` option is a one-time override.

## Installation
Install the latest released version:

```sh
go install github.com/painlesshippo/autofeat/cmd/autofeat@latest
```

Or build from a local clone:

```sh
git clone https://github.com/painlesshippo/autofeat.git
cd autofeat
go build -o autofeat ./cmd/autofeat
```

Ensure the installed binary directory, or the directory containing the built
binary, is on `PATH`.

`mise run install` installs the built binary into `$HOME/.local/bin` and adds
the `af`, `afr`, and `afl` aliases for `autofeat`, `autofeat review`, and
`autofeat list` to `$HOME/.bashrc` by default. Override these locations with
`INSTALL_DIR` and `SHELL_RC` when necessary.

## Development
Enable the repository's Git hooks after cloning:

```sh
mise run setup-hooks
```

The pre-commit hook verifies that staged Go, Markdown, shell, Go template, and
TOML files are formatted with `gofmt`, `rumdl`, `shfmt`, `gotmplfmt`, and
`taplo` through Mise. The commit-message hook enforces Conventional Commits.

## Releasing
The build uses `svu` to derive the current `MAJOR.MINOR.PATCH` version from Git
tags. Builds outside `main`, `master`, or `trunk` append the branch name as a
SemVer prerelease suffix, replacing characters such as `/` with `-`. Builds
with uncommitted changes append `-dirty`.

To create and publish a release, first commit all intended changes, switch to
`main`, `master`, or `trunk`, and ensure `origin` is reachable with Git
authentication:

```sh
mise run release
```

The command uses `svu next` to derive the next version from Conventional
Commits, runs the full test suite, creates and verifies an annotated
`vMAJOR.MINOR.PATCH` release tag, and pushes it to `origin`. It aborts before
creating a tag when the working tree is dirty, the current branch is not a
trunk branch, or remote authentication fails.

## Usage
Commands use `autofeat <command> [feature-selector ...]`. Existing-session
commands accept exact feature names, multiple selectors, `"*"` for every
feature, and patterns such as `"feature/*"`. Quote wildcard selectors so the
shell passes them to `autofeat` instead of expanding them as file names.

Create a feature session from inside a Git repository. The command creates a
branch with the supplied feature name, adds a worktree, records the session,
and regenerates its VS Code workspace. Feature names use valid Git branch
syntax, so hierarchical names such as `feature/potato` and `bug/f321s-aaa` are
supported.

The feature branch starts from the repository's cached `origin/<base>` when an
`origin` remote exists, or from the local base branch otherwise. Repository
addition does not fetch, so it remains available offline and uses the most
recently fetched base commit.

```sh
cd ~/sources/repo1
autofeat new feature/potato

cd ~/sources/repo2
autofeat new feature/potato
```

Add a remote repository to a feature from any directory by passing its HTTP(S)
or Git SSH URL. `autofeat` clones it into the feature directory and creates
the supplied feature branch:

```sh
autofeat new feature/potato https://github.com/example/repo3.git
```

Workspace directory names are flattened so they remain a directory.
`/` is escaped as `%2F` (and literal `%` as `%25`) to avoid collisions. With
the default configuration, this produces:

```text
~/.autofeat-workspaces/
└── feature%2Fpotato/
    ├── repo1/
    ├── repo2/
    ├── repo3/
      └── feature%2Fpotato.code-workspace
```

Open a session explicitly from any directory:

```sh
autofeat open feature/potato
```

Run the configured headless agent from a feature directory. When started from
a Git repository, `run` first adds that repository to the session when it has
not already been added. The agent inherits the terminal's standard input,
output, and error streams:

```sh
autofeat run feature/potato
```

Append an objective to the feature's `TASK.md` before starting the agent:

```sh
autofeat run feature/potato -task "Implement the potato feature and add tests"
```

Fetch each repository's configured base branch and rebase the feature onto it:

```sh
autofeat sync feature/potato
```

Every worktree in the session must be on the feature branch and have no staged,
unstaged, or untracked changes before synchronization begins. Repositories are
then synchronized sequentially. If a rebase conflicts, synchronization stops
and leaves the rebase in progress so it can be continued or aborted with the
Git commands printed by `autofeat`. Repositories without an `origin` remote
rebase onto their local base branch without fetching.

Generate and open a static HTML review of the changes in every active session.
Omitting the selector defaults to `"*"`. The review compares each worktree
with its merge-base against the stored base branch and includes committed,
staged, unstaged, and untracked changes. It also shows cached ahead and behind
counts without fetching. Each changed file is labeled as added, modified,
deleted, renamed, or copied, and its diff includes the whole file as context:

```sh
autofeat review
autofeat review "*"
```

Review one feature session or every feature whose name starts with
`feature/`:

```sh
autofeat review feature/potato
autofeat review "feature/*"
```

Running the feature command outside a Git repository also opens an existing
session:

```sh
cd ~
autofeat open feature/potato
```

List active sessions:

```sh
autofeat list
```

The `DRIFT` column reports cached base drift across each session's repositories.
Run `sync` to fetch current remote state and rebase; `list` itself never performs
network access.

Inspect every repository in every active session:

```sh
autofeat status
```

Pass one or more selectors to limit the report:

```sh
autofeat status feature/potato
autofeat status "feature/*"
```

`status` reports the current branch, clean or dirty worktree state, effective
base branch, cached base drift, cached push relationship, and an actionable
summary state. It never fetches or modifies repositories. For example:

```text
FEATURE          REPOSITORY  BRANCH          WORKTREE  BASE  DRIFT  PUSH         STATE        DETAIL
feature/potato   api         feature/potato  clean     main  +2/-0  unpublished  unpushed     -
feature/potato   web         feature/potato  dirty     main  +1/-2  +1/-0        behind-base  -
```

`DRIFT` compares `HEAD` with the cached base, while `PUSH` compares the feature
branch with its cached `origin` branch. `PUSH` is `unpublished` when `origin`
exists without a cached feature branch and `n/a` when no `origin` exists.
Summary states include `ready`, `dirty`, `behind-base`, `unpushed`,
`remote-ahead`, `remote-diverged`, `rebasing`, `wrong-branch`, `detached`,
`missing`, and `error`. The table retains repository inspection failures and
continues with the other repositories. A completed report exits successfully
regardless of repository health; invalid selectors and failures to load state
or write the report still return an error.

Use another branch as a one-time comparison override when needed:

```sh
autofeat review --base develop
autofeat review feature/potato --base develop
```

The command writes `$HOME/.autofeat/review.html`, opens it in the default
browser, and exits. It renders repository-level errors when the selected base
branch is unavailable, while continuing to render the other repositories. The
file is a snapshot: run `autofeat review` again after changes to refresh it.
Browser refresh only reloads the latest generated snapshot. Native Linux uses
`xdg-open`; WSL opens the snapshot through Windows `explorer.exe`.

Print the build version, commit, and timestamp:

```sh
autofeat version
```

Tear down a session after confirming all worktrees are clean:

```sh
autofeat teardown feature/potato
```

If a worktree or cloned remote repository has uncommitted changes, teardown
stops without removing anything. To intentionally discard those changes while
removing the worktrees, use:

```sh
autofeat teardown feature/potato --force
```

Commands can target multiple features or a pattern. For example:

```sh
autofeat teardown feature/x feature/y
autofeat sync "feature/*"
```

Before deleting a cloned remote repository, `autofeat` warns when its feature
branch has commits that have not been pushed to the corresponding remote branch.
