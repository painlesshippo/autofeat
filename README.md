# autofeat
`autofeat` manages disposable Git worktrees for concurrent AI-agent feature
development. A feature session can group worktrees from multiple repositories
and open them together in one VS Code workspace.

## Requirements
* Go 1.26.5 or later, only when building or installing the CLI from source.
* Mise, when using the source installation and development tasks.
* Git installed and available on `PATH`.
* VS Code's `code` command on `PATH` to use the default workspace-opening
  command. Another editor command can be configured instead.
* VS Code with the Dev Containers extension and a container runtime such as
  Docker to use `open --devcontainer PATH`. The configured editor command must
  understand `--file-uri`.
* GitHub Copilot CLI's `copilot` command on `PATH` to use `open --copilot` or
  the default headless-agent command.
* PowerShell 7 to use PowerShell argument completion on Windows.

On its first use, `autofeat` creates `$HOME/.autofeat/config.json`:

```json
{
  "schema_version": 1,
  "workspace_base_dir": "/home/your-user/.autofeat-workspaces",
  "editor_cmd": "code",
  "headless_cmd": "copilot",
  "hooks": [
    {
      "when": "post-add",
      "if_files": [
        "mise.toml",
        ".mise.toml"
      ],
      "run": "mise trust && mise install"
    }
  ]
}
```

Set `workspace_base_dir` to choose where feature worktrees and
`.code-workspace` files are created. Set `editor_cmd` to the executable that
should receive a workspace-file path. Set `headless_cmd` to the interactive
agent command used by `open --copilot` and `run`. Set `hooks` to commands that
run at supported lifecycle events. A `post-add` hook runs through `sh` with the
new local worktree or remote clone as its working directory. Its optional
`if_files` list limits execution to repositories containing at least one listed
file. By default, autofeat trusts and installs Mise projects after adding them.
Set `hooks` to `[]` to disable all hooks. If a hook fails, subsequent hooks do
not run and the new repository is removed so the operation can be retried.

`config.json`, `state.json`, and `templates.json` each include an independent
`schema_version`. Config and template files currently use version 1, while the
state file uses version 2. Existing unversioned files and version 1 state files
are accepted and upgraded the next time autofeat writes them. A file created by
a newer, unsupported schema requires a newer autofeat binary. Unknown fields
are rejected so manual editing mistakes do not silently discard data on the
next write.

`$HOME/.autofeat/state.json` stores `default_base_branch`, which defaults to
`master`, and `repository_base_branches`, which remembers base references
selected with `new --ref`. Despite their compatibility-preserving names, these
fields may contain any Git reference that resolves to a commit. When a
repository is added without a remembered reference, `autofeat` detects `master`
or `main` and records the result in the session as that repository's
`base_branch`. Change these values in the state file to use a different
persistent base reference, while preserving its `schema_version`.

## Installation
The release installers download the latest amd64 binary from
[GitHub Releases](https://github.com/painlesshippo/autofeat/releases/latest),
verify its SHA-256 checksum, install it, and add its directory to `PATH`. They
also install the bundled `autofeat` agent skill to
`$HOME/.agents/skills/autofeat`, where compatible agents can discover the
session lifecycle, safety, recovery, template, and configuration workflows.

### Install a release on Linux or WSL

```sh
curl -fsSL https://raw.githubusercontent.com/painlesshippo/autofeat/master/scripts/install-linux.sh | bash
```

The script installs to `$HOME/.local/bin` and configures Bash completion.
Append `-s -- --version 0.2.0` to install a specific version.

### Install a release on Windows

```powershell
irm https://raw.githubusercontent.com/painlesshippo/autofeat/master/scripts/install-win.ps1 | iex
```

The script installs to `$HOME\bin` and adds that directory to the user `PATH`.
It also enables argument completion in the current user's PowerShell 7 profile,
even when the installer is launched from Windows PowerShell 5.1. Set the
`VERSION` environment variable to install a specific version. Restart
PowerShell 7 or reload the profile path printed by the installer to activate
the new `PATH` and completion settings.

### Install from source on Linux or WSL

```sh
git clone https://github.com/painlesshippo/autofeat.git
cd autofeat
bash scripts/install-from-sources-linux.sh
```

`mise run install` is a shorthand for the same source installer.

The source installer uses Mise to install the pinned build tools before
building. Set `INSTALL_DIR` to override the installation directory and
`SHELL_RC` to override the Bash configuration file.

## Development
Enable the repository's Git hooks after cloning:

```sh
mise run setup-hooks
```

The pre-commit hook verifies that staged Go, Markdown, shell, Go template, and
TOML files are formatted with `gofmt`, `rumdl`, `shfmt`, `gotmplfmt`, and
`taplo` through Mise. The commit-message hook enforces Conventional Commits.

Run the unit tests and compiled CLI integration test together:

```sh
mise run test
```

The integration test builds a temporary `autofeat` executable and exercises a
complete local feature lifecycle against an isolated Git repository. Run only
that test with `mise run test-integration`. Installer and package validation is
kept separate in `mise run test-install`.

## Releasing
The build uses `svu` to derive the current `MAJOR.MINOR.PATCH` version from Git
tags. Builds outside `master` append the branch name as a SemVer prerelease
suffix, replacing characters such as `/` with `-`. Builds with uncommitted
changes append `-dirty`.

To create and publish a release, first commit all intended changes, switch to
`master`, and ensure `origin` is reachable with Git authentication:

```sh
mise run release
```

The command uses `svu next` to derive the next version from Conventional
Commits, runs the full test suite, creates and verifies an annotated
`vMAJOR.MINOR.PATCH` release tag, and pushes it to `origin`. The pushed tag
starts the GitHub Actions release workflow, which tests the tagged commit and
publishes Linux amd64 and Windows amd64 archives, generated release notes, and
`checksums.txt` to GitHub Releases. No additional GitHub secret is required.

The command aborts before creating a tag when the working tree is dirty, the
current branch is not `master`, or remote authentication fails. Validate the
packaging locally without publishing anything with:

```sh
mise run release-check
```

See [Release Implementation](docs/releases.md) for the complete tagging,
GitHub Actions, packaging, installer-validation, and recovery flow.

## Usage
Commands use `autofeat <command> [feature-selector ...]`. Selector-based
existing-session commands accept exact feature names, multiple selectors,
`"*"` for every feature, and patterns such as `"feature/*"`. Quote wildcard
selectors so the shell passes them to `autofeat` instead of expanding them as
file names. `remove` instead accepts one exact feature name and one repository
source.

Primary targets remain positional: feature names, feature selectors, template
names, and completion shells. Named flags specify sources, modes, secondary
inputs, or safety overrides. For example, `new` uses `--local`, `--remote`,
`--template`, and `--ref`; `remove` uses `--local` or `--remote`; `run` uses
`--task`; `sync` uses `--cancel`; and `remove` and `teardown` use `--force`.

| Command | Description |
| --- | --- |
| `autofeat new FEATURE [--local PATH \| --remote URL] [--ref REF]` | Create a feature session or add a repository to one. |
| `autofeat new FEATURE --template NAME` | Create a feature session from a saved template. |
| `autofeat new FEATURE --azure-build (ID \| URL) [--azure-org ORG --azure-project PROJECT] [--azure-url URL]` | Create a feature session from the repositories an Azure DevOps build resolved. |
| `autofeat remove FEATURE (--local PATH \| --remote URL) [--force]` | Remove one repository from a feature session. |
| `autofeat open SELECTOR... [--copilot \| --devcontainer PATH]` | Open matching sessions in the editor, the Dev Container at PATH, or Copilot CLI. |
| `autofeat run SELECTOR... [--task TEXT]` | Run the configured headless agent for matching sessions. |
| `autofeat sync SELECTOR...` | Fetch and rebase matching sessions onto their base references. |
| `autofeat sync SELECTOR... --cancel` | Abort in-progress rebases left by an interrupted synchronization. |
| `autofeat status [SELECTOR...]` | Inspect repository health; defaults to every session. |
| `autofeat teardown SELECTOR... [--force]` | Remove matching sessions and their worktrees. |
| `autofeat list` | List active sessions and their cached base drift. |
| `autofeat template list` | List saved templates. |
| `autofeat template show NAME` | Show the repositories in a template. |
| `autofeat template save NAME --from FEATURE` | Save an active session as a template. |
| `autofeat config` | Open the global configuration in the configured editor. |
| `autofeat version` | Print build version information. |
| `autofeat completion bash` | Generate the Bash completion script. |
| `autofeat completion powershell` | Generate the PowerShell completion script. |

Open the global configuration in the configured editor:

```sh
autofeat config
```

Bash and PowerShell completion suggest active feature names for `new`,
`remove`, `open`, `run`, `sync`, `status`, and `teardown`. This lets `new` add
a repository to an existing feature; a feature name that does not exist yet
remains free-form. Commands that accept multiple selectors omit names already
selected. Completion also includes each command's supported options.

Print a completion script for manual shell setup with:

```sh
autofeat completion bash
autofeat completion powershell
```

In PowerShell, evaluate the second command with:

```powershell
(& autofeat completion powershell) -join "`n" | Invoke-Expression
```

See [PowerShell Autocomplete](docs/powershell-autocomplete.md) for profile
setup, behavior, and troubleshooting details.

Save an active session's ordered repository group as a reusable template:

```sh
autofeat template save full-stack --from feature/potato
```

Templates retain each repository's local source path or remote URL, but not its
generated worktree path, base branch, or feature name. They are stored in
`$HOME/.autofeat/templates.json`. List templates or inspect their sources with:

```sh
autofeat template list
autofeat template show full-stack
```

Create a new session containing every repository in a template:

```sh
autofeat new feature/new-checkout --template full-stack
```

Local repositories receive worktrees and remote repositories are cloned in the
stored order. Remembered base references are reused where available; otherwise,
base branches are detected when the new session is created. Normal `post-add`
hooks run for every repository. All sources are checked before creation begins.
If adding a later repository fails, autofeat removes the worktrees, clones, and
branches it already created for that session. Template creation requires a new
feature name and does not append to an existing session.

Create a feature session from inside a Git repository. The command reuses or
creates a branch with the supplied feature name, adds a worktree, records the
session, and regenerates its VS Code workspace. Feature names use valid Git
branch syntax, so hierarchical names such as `feature/potato` and
`bug/f321s-aaa` are supported.

With no repository source flag, `new` uses the Git repository containing the
current directory. Use `--local` to select another local repository without
changing directories. The path may be the repository root or a directory
inside it:

```sh
autofeat new feature/potato --local ~/sources/repo1
autofeat new feature/potato --local ../repo2/packages/api --ref develop
```

Local repositories always use Git worktrees; they are not cloned.

An existing local feature branch is reused at its current tip. If no local
branch exists but a cached `origin/<feature>` branch does, autofeat creates the
local feature branch from that cached commit without configuring an upstream.
Otherwise, the feature branch is created from the selected base reference.
Repository addition does not fetch. A feature branch that is already checked
out in another worktree remains there, and Git reports the checkout conflict.

For an unqualified base branch name, a newly created feature branch starts from
the repository's cached `origin/<branch>` when it exists, or from the local base
branch otherwise. Repository addition does not fetch, so it remains available
offline and uses the most recently fetched commit.

Select and remember a different base reference for the current repository with
`--ref`. Branches, tags, commit SHAs, full refs, and Git revision expressions
are accepted when they resolve to a commit:

```sh
autofeat new feature/potato --ref develop
autofeat new feature/from-release --ref v1.2.3
autofeat new feature/from-commit --ref 8d13fc2
autofeat new feature/from-tag --ref refs/tags/v1.2.3
```

Future sessions created from the same canonical repository path reuse
the reference. Another explicit `--ref` replaces the remembered value. Base
selection uses an explicit ref first, then the remembered repository ref, then
detected `master` or `main`, and finally `default_base_branch`. When a tag and
branch have the same unqualified name, the branch wins; use a full tag ref to
disambiguate it. Session state stores selected tags as full tag refs and expands
abbreviated commit IDs so an existing session's base remains stable.

```sh
cd ~/sources/repo1
autofeat new feature/potato

cd ~/sources/repo2
autofeat new feature/potato
```

Add a remote repository to a feature from any directory with `--remote` and an
HTTP(S) or Git SSH URL. `autofeat` always clones it into the feature directory,
even when another local clone exists, and creates the supplied feature branch:

```sh
autofeat new feature/potato --remote https://github.com/example/repo3.git
autofeat new feature/other --remote https://github.com/example/repo3.git --ref develop
```

Remote repository preferences are remembered by their normalized URL.

Create a feature session from an Azure DevOps build with `--azure-build`.
`autofeat` resolves the build's primary repository and its Git repository
resources, then clones each one into the feature directory and pins the created
feature branch to the exact commit that build used, rather than the current
branch tip. Point at a build with either its numeric id plus `--azure-org` and
`--azure-project`, or a build results URL:

```sh
autofeat new feature/repro --azure-build 4821 --azure-org contoso --azure-project Widgets
autofeat new feature/repro --azure-build "https://dev.azure.com/contoso/Widgets/_build/results?buildId=4821"
```

Azure DevOps Server (on-premises) instances pass the collection as
`--azure-org` and the server prefix as `--azure-url` with the numeric form, for
example `--azure-org DefaultCollection --azure-url https://server/tfs`. Each
repository is recorded as a normal remote clone whose `base_branch` is the
resolved commit, so `list`, `status`, `open`, and `teardown` behave as they do
for any other session. Repositories that the build references more than once,
including overlap between the primary repository and a resource, are cloned
once; a repository the build resolves to two different commits is an error.

The exact commit for each repository always comes from the pipeline run, which
is authoritative for the revision a build used. Repository identity and clone
URL come from the Build API for the primary repository, and for repository
resources from the run's expanded pipeline definition (its `finalYaml`), whose
`resources.repositories` declarations name each repository. Because the YAML
name carries the project, resources in other projects of the organization are
supported. Classic (designer) builds have only their primary repository.

Set `AZURE_DEVOPS_EXT_PAT` (or `AZURE_DEVOPS_PAT`) to an Azure DevOps personal
access token with **Build (Read)** scope before running the command. That scope
covers every REST call autofeat makes to resolve the build (build, definition,
and pipeline run). The token is only ever sent as an Authorization header and is
never written to session metadata, printed, or embedded in a stored clone URL.
Cloning and fetching are separate and use Git's normal credential configuration,
exactly like `--remote`, so configure a Git credential helper (or SSH keys) with
read access to every repository the build uses. A personal access token reused
as the Git password for Azure Repos over HTTPS needs **Code (Read)** scope for
that clone step; the Build (Read) REST token does not clone.

This command covers only Git repositories. Build artifacts and non-repository
pipeline resources are out of scope. Repository resources of the Azure Repos
(`git`), GitHub, and Bitbucket types are supported through their `finalYaml`
declarations; a self-hosted type without a derivable clone host (for example
GitHub Enterprise) or any non-Git type produces an actionable error rather than
a silently incomplete workspace, as does a run whose resources cannot be
resolved. A build revision that lives on a ref a default clone does not fetch,
such as a pull request merge ref, is retrieved from the resolved ref name when
the server exposes it; when the exact commit cannot be made available,
`autofeat` reports an error and leaves no partial session behind.

Workspace directory names are flattened so they remain a single directory.
Every character outside `A-Z`, `a-z`, `0-9`, `.`, `_` and `-` becomes `-`,
repeated dashes collapse, and leading or trailing `-` and `.` are trimmed, so
the name stays portable and free of percent escapes. This is lossy: if a new
feature normalizes to a directory an existing session already uses, `autofeat`
refuses to create it and asks for a different feature name. With the default
configuration, `feature/potato` produces:

```text
~/.autofeat-workspaces/
└── feature-potato/
    ├── repo1/
    ├── repo2/
    ├── repo3/
      └── feature-potato.code-workspace
```

Open a session explicitly from any directory:

```sh
autofeat open feature/potato
```

Open the feature's generated workspace inside a Dev Container instead of on the
host by pointing `--devcontainer` at a `devcontainer.json`:

```sh
autofeat open feature/potato --devcontainer ./service/.devcontainer/devcontainer.json
```

`--devcontainer PATH` opens the session's generated multi-root `.code-workspace`
in the Dev Container defined by the configuration at `PATH`, through the editor's
`vscode-remote://dev-container+` file URI, building or attaching the container as
needed. There is no automatic discovery — the path is required and explicit.

The configuration is developer-owned and passed through untouched: autofeat does
not create, copy, merge, rewrite, or parse it, and does not inject mounts or
adjust workspace settings. The configuration owns the container's mounts, its
handling of the session's linked-worktree Git metadata (worktree Git data lives
outside the feature directory, so the configuration must mount it if Git is
needed in the container), its `localWorkspaceFolder` semantics, and its
compatibility with the session layout. autofeat opens the feature directory as
the container's local workspace folder and opens the workspace file at VS Code's
default mount location, `/workspaces/<feature-dir-name>/<workspace>.code-workspace`.

`PATH` may live anywhere, including outside the feature session. A relative path
is resolved against the current working directory (so it behaves like any other
shell path argument), and one command that opens several selected sessions
applies the same configuration to each. A missing, non-file, or unreadable path
fails immediately with an actionable error instead of falling back to a host
window; autofeat validates only that the path is a readable file and never
judges the configuration's container semantics.

The configured `editor_cmd` must be a VS Code-family command (`code`,
`code-insiders`, `cursor`, and so on) that understands `--file-uri`. Under WSL,
the feature and configuration paths are translated with `wslpath` so the
Windows-side Dev Containers extension can locate them.

Open GitHub Copilot CLI in the invoking terminal with the feature directory as
its workspace root. The CLI remains interactive and receives no initial
prompt:

```sh
autofeat open feature/potato --copilot
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
autofeat run feature/potato --task "Implement the potato feature and add tests"
```

Synchronize each repository with its configured base reference:

```sh
autofeat sync feature/potato
```

Every worktree in the session must be on the feature branch and have no staged,
unstaged, or untracked changes before synchronization begins. Repositories are
then synchronized sequentially. If a rebase conflicts, synchronization stops
and leaves the rebase in progress so it can be resolved and continued, or
cancelled with `autofeat sync --cancel`. Repositories without an `origin`
remote rebase onto their local base branch without fetching. Origin branches
are fetched before rebasing; tags, commit SHAs, and other immutable references
are resolved locally and are not fetched.

Cancel an interrupted synchronization when a conflict is not worth resolving
right now:

```sh
autofeat sync feature/potato --cancel
```

Cancellation aborts the rebase in progress in every repository of each selected
session with `git rebase --abort`, restoring the worktree, its branch, and its
pre-rebase commit. Repositories without a rebase in progress are reported as
`no rebase in progress` and are left untouched; repositories and sessions
outside the selectors are never modified. Every selected session is processed
even when one fails, and each failure is reported with its repository name and
worktree path, so a partial cancellation never exits successfully. Cancellation
only aborts rebases: it never discards committed feature work, and without
`--cancel` the `sync` command behaves exactly as before.

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

Print the build version, commit, and timestamp:

```sh
autofeat version
```

Remove one local or remote repository from a feature by specifying the same
source used to add it:

```sh
autofeat remove feature/potato --local ~/sources/repo1
autofeat remove feature/potato --remote https://github.com/example/repo3.git
```

A local path may point anywhere inside the original repository. Removal stops
when the selected worktree has uncommitted changes; use `--force` to discard
them. Before deleting a remote clone, `autofeat` warns about commits that have
not been pushed. Local feature branches are retained. Removing the final
repository tears down the complete feature session and runs normal
`post-teardown` hooks.

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
autofeat sync "feature/*" --cancel
```

Before deleting a cloned remote repository, `autofeat` warns when its feature
branch has commits that have not been pushed to the corresponding remote branch.
