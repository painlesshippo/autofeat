#!/usr/bin/env bash

set -euo pipefail

COMMIT_SHA="$(git rev-parse --verify HEAD)"
VERSION="$(svu current)"
VERSION="${VERSION#v}"

BRANCH_NAME="$(git symbolic-ref --quiet --short HEAD || true)"
if [[ -n "$BRANCH_NAME" && "$BRANCH_NAME" != master ]]; then
    BRANCH_SUFFIX="$(printf '%s' "$BRANCH_NAME" | LC_ALL=C tr -c 'A-Za-z0-9-' '-')"
    if [[ "$BRANCH_SUFFIX" =~ ^0[0-9]+$ ]]; then
        BRANCH_SUFFIX="branch-$BRANCH_SUFFIX"
    fi
    VERSION="${VERSION}-${BRANCH_SUFFIX}"
fi

if [[ -n "$(git status --porcelain)" ]]; then
    VERSION="${VERSION}-dirty"
fi
