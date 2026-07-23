#!/usr/bin/env bash

set -euo pipefail

mkdir -p bin

# Sets VERSION and COMMIT_SHA.
source ./scripts/version.sh

BUILD_DATETIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION=$(go version | awk '{print $3}')

echo "Building autofeat v$VERSION ($COMMIT_SHA), at $BUILD_DATETIME with $GO_VERSION"

LDFLAGS="-X main.version=$VERSION -X main.commit=$COMMIT_SHA -X main.buildDatetime=$BUILD_DATETIME -X main.goVersion=$GO_VERSION"

go build -ldflags "$LDFLAGS" -o bin/autofeat ./cmd/autofeat
