#!/usr/bin/env bash
# Runs the compiled binary in hosted mode against .env.local's values -
# nothing auto-loads that file otherwise (vercel dev does; a plain
# `./coc-progress` does not). Build first if you've changed Go code; this
# doesn't rebuild it for you.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a
source .env.local
set +a
exec ./coc-progress
