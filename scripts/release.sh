#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

if [[ -n "$(git status --porcelain)" ]]; then
    echo "Release aborted: the working tree must be clean." >&2
    exit 1
fi

branch_name="$(git symbolic-ref --quiet --short HEAD || true)"
case "$branch_name" in
main | master | trunk) ;;
*)
    echo "Release aborted: releases must be created from main, master, or trunk." >&2
    exit 1
    ;;
esac

if ! git remote get-url origin >/dev/null 2>&1; then
    echo "Release aborted: no origin remote is configured." >&2
    exit 1
fi

if ! git ls-remote --exit-code origin HEAD >/dev/null 2>&1; then
    echo "Release aborted: unable to authenticate with origin." >&2
    exit 1
fi

release_tag="$(svu next)"
release_version="${release_tag#v}"

if git rev-parse --verify --quiet "refs/tags/$release_tag" >/dev/null; then
    echo "Release aborted: tag $release_tag already exists." >&2
    exit 1
fi

echo "Preparing release $release_tag"
go test -v ./...
git tag -a "$release_tag" -m "Release $release_tag"

if ! bash ./scripts/build.sh; then
    git tag -d "$release_tag"
    exit 1
fi

if ! ./bin/autofeat version | grep -Fxq "autofeat $release_version"; then
    git tag -d "$release_tag"
    echo "Release aborted: built binary did not report $release_version." >&2
    exit 1
fi

git push origin "$release_tag"
echo "Published $release_tag"
