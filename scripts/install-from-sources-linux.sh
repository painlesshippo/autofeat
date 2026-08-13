#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

if ! command -v mise >/dev/null 2>&1; then
    echo "Mise is required to install autofeat from source." >&2
    exit 1
fi

mise install
mise exec -- bash ./scripts/build.sh
BINARY_PATH="$project_root/bin/autofeat" bash ./scripts/install-linux.sh
