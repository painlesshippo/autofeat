#!/usr/bin/env bash

set -euo pipefail

mkdir -p bin

gitversion > bin/gitversion.json

VERSION=$(jq -r '.SemVer' bin/gitversion.json)
COMMIT_SHA=$(jq -r '.Sha' bin/gitversion.json)
BUILD_DATETIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Building autofeat version $VERSION ($COMMIT_SHA), built at $BUILD_DATETIME"

LDFLAGS="-X main.version=$VERSION -X main.commit=$COMMIT_SHA -X main.buildDatetime=$BUILD_DATETIME"

go build -ldflags "$LDFLAGS" -o bin/autofeat ./cmd/autofeat
