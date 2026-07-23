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
the `af`, `afp`, and `afl` aliases for `autofeat`, `autofeat review`, and
`autofeat list` to `$HOME/.bashrc` by default. Override these locations with
`INSTALL_DIR` and `SHELL_RC` when necessary.

## Development

Enable the repository's Git hooks after cloning:

```sh
mise run setup-hooks
```

The pre-commit hook verifies that staged Go files are formatted with `gofmt`
and runs `rumdl check .` through Mise to lint all Markdown files.

## Releasing

The build derives the application version from annotated Git tags using
GitVersion. Do not edit a version constant to make a release. Choose the next
Semantic Versioning number: increment the patch for compatible fixes, the minor
version for compatible features, or the major version for breaking changes.

To create and publish the next minor release, first commit all intended changes,
start from a clean working tree, and ensure `origin` is reachable with Git
authentication:

```sh
mise run release-minor
```

The command derives the next minor version from the most recent stable release
tag, runs the full test suite, creates and verifies an annotated release tag,
and pushes it to `origin`. It aborts before creating a tag when the working
tree is dirty or remote authentication fails.

## Usage

Run `autofeat <feature-name>` inside a Git repository to add that repository
to a feature session. The command creates a branch with the supplied feature
name, adds a worktree, records the session, and regenerates its VS Code
workspace. Feature names use valid Git branch syntax, so hierarchical names
such as `feature/potato` and `bug/f321s-aaa` are supported.

```sh
cd ~/sources/repo1
autofeat feature/potato

cd ~/sources/repo2
autofeat feature/potato
```

Add a remote repository to a feature from any directory by passing its HTTP(S)
or Git SSH URL. `autofeat` clones it into the feature directory and creates
the supplied feature branch:

```sh
autofeat feature/potato https://github.com/example/repo3.git



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
autofeat feature/potato open
```

Run the configured headless agent from a feature directory. When started from
a Git repository, `run` first adds that repository to the session when it has
not already been added. The agent inherits the terminal's standard input,
output, and error streams:

```sh
autofeat feature/potato run
```

Append an objective to the feature's `TASK.md` before starting the agent:

```sh
autofeat feature/potato run -task "Implement the potato feature and add tests"
```

Generate and open a static HTML review of the changes in every active session.
The review compares each worktree with its stored base branch and includes
committed, staged, unstaged, and untracked changes:

```sh
autofeat review
```

Review only one feature session:

```sh
autofeat feature/potato review
```

Running the feature command outside a Git repository also opens an existing
session:

```sh
cd ~
autofeat feature/potato
```

List active sessions:

```sh
autofeat list
```

Use another branch as a one-time comparison override when needed:

```sh
autofeat review --base develop
autofeat feature/potato review --base develop
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
autofeat feature/potato teardown
```

If a worktree or cloned remote repository has uncommitted changes, teardown
stops without removing anything. To intentionally discard those changes while
removing the worktrees, use:

```sh
autofeat feature/potato teardown --force
```

Before deleting a cloned remote repository, `autofeat` warns when its feature
branch has commits that have not been pushed to the corresponding remote branch.
