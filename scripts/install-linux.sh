#!/usr/bin/env bash

set -euo pipefail

version="${VERSION:-}"
while [[ $# -gt 0 ]]; do
    case "$1" in
    --version)
        if [[ $# -lt 2 ]]; then
            echo "Missing value for --version." >&2
            exit 1
        fi
        version="$2"
        shift 2
        ;;
    *)
        echo "Unknown argument: $1" >&2
        exit 1
        ;;
    esac
done

temporary_dir=""
cleanup() {
    if [[ -n "$temporary_dir" ]]; then
        rm -rf "$temporary_dir"
    fi
}
trap cleanup EXIT

binary_path="${BINARY_PATH:-}"
skill_path="${SKILL_PATH:-}"
skill_path_required=false
if [[ -n "$skill_path" ]]; then
    skill_path_required=true
fi
if [[ -z "$binary_path" ]]; then
    repository_url="https://github.com/painlesshippo/autofeat"
    if [[ -z "$version" ]]; then
        latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$repository_url/releases/latest")"
        release_tag="${latest_url##*/}"
        if [[ "$release_tag" != v* ]]; then
            echo "Unable to determine the latest autofeat release." >&2
            exit 1
        fi
        version="${release_tag#v}"
    fi
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
        echo "Invalid release version: $version" >&2
        exit 1
    fi

    archive="autofeat_${version}_linux_amd64.tar.gz"
    release_url="$repository_url/releases/download/v${version}"
    temporary_dir="$(mktemp -d)"
    curl -fsSL "$release_url/$archive" -o "$temporary_dir/$archive"
    curl -fsSL "$release_url/checksums.txt" -o "$temporary_dir/checksums.txt"

    checksum_line="$(awk -v archive="$archive" '$2 == archive { print; exit }' "$temporary_dir/checksums.txt")"
    if [[ -z "$checksum_line" ]]; then
        echo "No checksum found for $archive." >&2
        exit 1
    fi
    (cd "$temporary_dir" && printf '%s\n' "$checksum_line" | sha256sum -c -)
    tar -xzf "$temporary_dir/$archive" -C "$temporary_dir" autofeat
    binary_path="$temporary_dir/autofeat"
    if [[ -z "$skill_path" ]]; then
        if tar -tzf "$temporary_dir/$archive" skills/autofeat/SKILL.md >/dev/null 2>&1; then
            tar -xzf "$temporary_dir/$archive" -C "$temporary_dir" skills/autofeat
            skill_path="$temporary_dir/skills/autofeat"
        else
            echo "Agent skill is not bundled with autofeat $version; skipping skill installation." >&2
        fi
    fi
elif [[ ! -f "$binary_path" ]]; then
    echo "Binary not found: $binary_path" >&2
    exit 1
fi
if [[ "$skill_path_required" == true && ! -f "$skill_path/SKILL.md" ]]; then
    echo "Agent skill not found: $skill_path" >&2
    exit 1
fi

install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
skill_dir="${SKILL_DIR:-$HOME/.agents/skills}"
skill_target="$skill_dir/autofeat"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""
completion_source="source <(autofeat completion bash)"

mkdir -p "$install_dir"
install -m 0755 "$binary_path" "$install_dir/autofeat"
if [[ -n "$skill_path" ]]; then
    mkdir -p "$skill_dir"
    rm -rf "$skill_target"
    cp -R "$skill_path" "$skill_target"
fi

add_path=false
case ":$PATH:" in
*":$install_dir:"*) ;;
*) add_path=true ;;
esac

add_completion=false
if ! grep -Fqx "$completion_source" "$shell_rc" 2>/dev/null; then
    add_completion=true
fi

if [[ "$add_path" == true || "$add_completion" == true ]]; then
    mkdir -p "$(dirname "$shell_rc")"
    {
        echo
        echo "# Added by autofeat install"
        if [[ "$add_path" == true ]]; then
            echo "$path_export"
        fi
        if [[ "$add_completion" == true ]]; then
            echo "$completion_source"
        fi
    } >>"$shell_rc"
fi

if [[ "$add_path" == true ]]; then
    echo "Added $install_dir to PATH in $shell_rc"
fi
if [[ "$add_completion" == true ]]; then
    echo "Added autofeat Bash completion in $shell_rc"
fi
if [[ "$add_path" == true || "$add_completion" == true ]]; then
    echo "Run: source $shell_rc"
fi

echo "Installed $install_dir/autofeat"
if [[ -n "$skill_path" ]]; then
    echo "Installed $skill_target"
fi
