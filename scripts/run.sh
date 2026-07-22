#!/usr/bin/env bash

# $# is the number of arguments passed to the script

set -euo pipefail

if [ "$#" -eq 0 ]; then
	./bin/autofeat list
else
	./bin/autofeat "$@"
fi