#!/usr/bin/env bash

set -euo pipefail

mkdir -p bin

gitversion > bin/gitversion.json

VERSION=$(jq -r '.SemVer' bin/gitversion.json)
COMMIT_SHA=$(jq -r '.Sha' bin/gitversion.json)
BUILD_DATETIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION=$(go version | awk '{print $3}')

echo "Building autofeat v$VERSION ($COMMIT_SHA), at $BUILD_DATETIME with $GO_VERSION"

LDFLAGS="-X main.version=$VERSION -X main.commit=$COMMIT_SHA -X main.buildDatetime=$BUILD_DATETIME -X main.goVersion=$GO_VERSION"

go build -ldflags "$LDFLAGS" -o bin/autofeat ./cmd/autofeat
