#!/bin/sh
set -eu

if [ $# -lt 1 ]; then
  echo "Usage: $0 \"commit message\""
  exit 1
fi

MSG="$1"

# Format + quality gates
if command -v go >/dev/null 2>&1; then
  GOTOOLCHAIN=auto gofmt -w $(find . -type f -name '*.go' -not -path './vendor/*')
  GOTOOLCHAIN=auto go vet ./...
  GOTOOLCHAIN=auto go test ./...
  GOTOOLCHAIN=auto go build ./...
else
  echo "go not found; aborting commit"
  exit 1
fi

# Commit + push
git add -u
git commit -m "$MSG"
git push origin master
