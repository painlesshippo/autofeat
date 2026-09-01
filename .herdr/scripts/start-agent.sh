#!/usr/bin/env bash

set -euo pipefail

if [[ "${HERDR_ENV:-}" != "1" ]]; then
    echo "start-agent.sh must run inside a Herdr-managed pane" >&2
    exit 1
fi

if (($# < 5)); then
    echo "usage: $0 <name> <kind> <pane-id> <role> <context-file> [agent-args...]" >&2
    exit 2
fi

name="$1"
kind="$2"
pane_id="$3"
role="$4"
context_file="$5"
shift 5

case "$role" in
orchestrator | coder | reviewer) ;;
*)
    echo "unknown role: $role" >&2
    exit 2
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
project_root="$(cd "${script_dir}/../.." && pwd -P)"
role_file="${project_root}/.herdr/agents/${role}.md"

if [[ ! -f "$role_file" ]]; then
    echo "unknown role instructions: $role_file" >&2
    exit 2
fi

if [[ ! -f "$context_file" ]]; then
    echo "context file does not exist: $context_file" >&2
    exit 2
fi

if (($# > 0)); then
    herdr agent start "$name" --kind "$kind" --pane "$pane_id" -- "$@"
else
    herdr agent start "$name" --kind "$kind" --pane "$pane_id"
fi

prompt="$({
    printf '%s\n\n' "Role instructions:"
    cat "$role_file"
    printf '\n\n%s\n\n' "Task context:"
    cat "$context_file"
})"

herdr agent prompt "$name" "$prompt"
