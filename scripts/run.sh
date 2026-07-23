#!/usr/bin/env bash

# $# is the number of arguments passed to this script
# $@ is the list of arguments passed to this script

set -euo pipefail

if [ "$#" -eq 0 ]; then
    ./bin/autofeat version
else
    ./bin/autofeat "$@"
fi
