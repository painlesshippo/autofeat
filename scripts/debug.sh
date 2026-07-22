#!/usr/bin/env bash

set -euo pipefail

autofeat_dir="${AUTOFEAT_DIR:-$HOME/.autofeat}"
config_path="$autofeat_dir/config.json"
state_path="$autofeat_dir/state.json"

printf 'Config: %s\n' "$config_path"
if [[ -f "$config_path" ]]; then
    cat "$config_path"
else
    echo "(not found)"
fi

echo
printf 'State: %s\n' "$state_path"
if [[ -f "$state_path" ]]; then
    cat "$state_path"
else
    echo "(not found)"
fi
