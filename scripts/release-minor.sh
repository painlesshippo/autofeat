#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

if [[ -n "$(git status --porcelain)" ]]; then
    echo "Release aborted: the working tree must be clean." >&2
    exit 1
fi

if ! git remote get-url origin >/dev/null 2>&1; then
    echo "Release aborted: no origin remote is configured." >&2
    exit 1
fi

if ! git ls-remote --exit-code origin HEAD >/dev/null 2>&1; then
    echo "Release aborted: unable to authenticate with origin." >&2
    exit 1
fi

latest_tag="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-version:refname |
    grep -Ex 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n 1)"

if [[ -z "$latest_tag" ]]; then
    echo "Release aborted: no stable v<major>.<minor>.<patch> tag exists." >&2
    exit 1
fi

version="${latest_tag#v}"
IFS='.' read -r major minor _ <<<"$version"
next_tag="v${major}.$((minor + 1)).0"

if git rev-parse --verify --quiet "refs/tags/$next_tag" >/dev/null; then
    echo "Release aborted: tag $next_tag already exists." >&2
    exit 1
fi

echo "Preparing minor release $next_tag from $latest_tag"
go test -v ./...
git tag -a "$next_tag" -m "Release $next_tag"

if ! bash ./scripts/build.sh; then
    git tag -d "$next_tag"
    exit 1
fi

if ! ./bin/autofeat version | grep -Fxq "autofeat ${next_tag#v}"; then
    git tag -d "$next_tag"
    echo "Release aborted: built binary did not report ${next_tag#v}." >&2
    exit 1
fi

git push origin "$next_tag"
echo "Published $next_tag"
