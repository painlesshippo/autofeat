#!/bin/bash
# usage: ./make.sh <function>
# example: ./make.sh help

set -eou pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$PROJECT_ROOT/bin"
BINARY_PATH="$BIN_DIR/autofeat"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SHELL_RC="${SHELL_RC:-$HOME/.bashrc}"

function help() {
    echo "Usage: $0 <function>"
    echo "Available functions:"
    echo "  build   - Build the project"
    echo "  run     - Build and run the CLI"
    echo "  clean   - Clean build artifacts"
    echo "  install - Install the CLI and add it to PATH"
    echo "  uninstall - Remove the installed CLI and PATH entry"
    echo "  test    - Run tests"
}

function build() {
    mkdir -p "$BIN_DIR"
    (
        cd "$PROJECT_ROOT"
        go build -o "$BINARY_PATH" ./cmd/autofeat
    )
    chmod +x "$BINARY_PATH"
    echo "Built $BINARY_PATH"
}

function run() {
    build
    "$BINARY_PATH"
}

function clean() {
    rm -rf "$BIN_DIR"
    echo "Cleaned $BIN_DIR"
}

function install() {
    local path_export="export PATH=\"$INSTALL_DIR:\$PATH\""

    build
    mkdir -p "$INSTALL_DIR"
    cp "$BINARY_PATH" "$INSTALL_DIR/autofeat"
    chmod +x "$INSTALL_DIR/autofeat"

    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            mkdir -p "$(dirname "$SHELL_RC")"
            if ! grep -Fqx "$path_export" "$SHELL_RC" 2>/dev/null; then
                {
                    echo
                    echo "# Added by autofeat install"
                    echo "$path_export"
                } >> "$SHELL_RC"
            fi
            echo "Added $INSTALL_DIR to PATH in $SHELL_RC"
            echo "Run: source $SHELL_RC"
            ;;
    esac

    echo "Installed $INSTALL_DIR/autofeat"
}

function uninstall() {
    local path_export="export PATH=\"$INSTALL_DIR:\$PATH\""

    rm -f "$INSTALL_DIR/autofeat"

    if [[ -f "$SHELL_RC" ]] && grep -Fqx "# Added by autofeat install" "$SHELL_RC"; then
        local temporary_rc
        temporary_rc="$(mktemp "$SHELL_RC.XXXXXX")"
        awk -v marker="# Added by autofeat install" -v path_export="$path_export" '
            $0 == marker {
                remove_next = 1
                next
            }
            remove_next && $0 == path_export {
                remove_next = 0
                next
            }
            {
                remove_next = 0
                print
            }
        ' "$SHELL_RC" > "$temporary_rc"
        mv "$temporary_rc" "$SHELL_RC"
        echo "Removed $INSTALL_DIR from PATH in $SHELL_RC"
        echo "Run: source $SHELL_RC"
    fi

    echo "Uninstalled $INSTALL_DIR/autofeat"
}

function test() {
    go test -v ./...
}

"$@"