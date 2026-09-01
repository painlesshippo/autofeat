#!/usr/bin/env bash

set -euo pipefail

if [[ "${HERDR_ENV:-}" != "1" ]]; then
    echo "create-workspace.sh must run inside a Herdr-managed pane" >&2
    exit 1
fi

if (($# < 1 || $# > 4)); then
    echo "usage: $0 <slug> [base] [workspace-root] [branch-prefix]" >&2
    exit 2
fi

slug="$1"

case "$slug" in
"" | -* | *- | *[!a-z0-9-]*)
    echo "slug must contain only lowercase letters, digits, and interior hyphens" >&2
    exit 2
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
config_file="${script_dir}/../config.yml"

if [[ ! -f "$config_file" ]]; then
    echo "Herdr configuration not found: $config_file" >&2
    exit 1
fi

config() {
    yq eval --exit-status "$1" "$config_file"
}

repo_root="$(git rev-parse --show-toplevel)"
project_name="$(basename "$repo_root")"
repo_parent="$(dirname "$repo_root")"
workspace_root="${3:-${repo_parent}/${project_name}-workspaces}"
branch_prefix="${4:-herdr/}"

# An implicit base must come from the remote tip, not a possibly stale local
# branch. Keep the fetched ref in this workflow's namespace and leave the
# origin remote's tracking namespace untouched.
if (($# >= 2)) && [[ -n "$2" ]]; then
    base="$2"
    echo "using the requested base ref: $base" >&2
else
    if ! base_branch="$(config '.project.base_branch')"; then
        echo "project.base_branch is not set in $config_file" >&2
        exit 1
    fi

    if ! push_url="$(config '.project.push_url')"; then
        echo "project.push_url is not set in $config_file" >&2
        echo "it is required to fetch a fresh ${base_branch}; pass an explicit base to branch without fetching" >&2
        exit 1
    fi

    base="refs/herdr/base/${base_branch}"

    if ! git -C "$repo_root" fetch --no-tags "$push_url" \
        "+refs/heads/${base_branch}:${base}"; then
        echo "failed to fetch ${base_branch} from ${push_url}" >&2
        echo "refusing to create a workspace from the possibly stale local ${base_branch}" >&2
        echo "fix the fetch, or pass an explicit base to branch from a chosen ref deliberately" >&2
        exit 1
    fi
fi

base_sha="$(git -C "$repo_root" rev-parse --verify "${base}^{commit}")"
echo "base ${base} is ${base_sha}" >&2

mkdir -p "$workspace_root"
workspace_root="$(cd "$workspace_root" && pwd -P)"
workspace_path="${workspace_root}/${slug}"
branch="${branch_prefix}${slug}"

if [[ -e "$workspace_path" ]]; then
    echo "workspace path already exists: $workspace_path" >&2
    exit 1
fi

if git -C "$repo_root" show-ref --verify --quiet "refs/heads/${branch}"; then
    echo "branch already exists: $branch" >&2
    exit 1
fi

herdr worktree create \
    --cwd "$repo_root" \
    --branch "$branch" \
    --base "$base" \
    --path "$workspace_path" \
    --label "${project_name}: ${slug}" \
    --no-focus
