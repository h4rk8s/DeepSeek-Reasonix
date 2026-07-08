#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "== TUI interaction regression tests =="
go test -count=1 ./internal/cli -run 'Test(Empty.*Reasoning|IngestSeparatesReasoning|LazyReasoning|ImageUnderstanding|ReplayHistory|Paste.*Image|TypedAtShellEscapedImagePath|JumpToBottom|CtrlEnd|Click.*Jump|MouseSelection|RunningStreamPreserves|CollapsedHover|MouseClickKeeps)'

echo "== Build release binaries =="
make build

echo "== Smoke binary metadata =="
./bin/reasonix --version
