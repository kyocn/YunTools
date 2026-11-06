#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
unformatted=$(gofmt -l ./*.go internal cmd)
if [ -n "$unformatted" ]; then
  echo "Go files require formatting:"
  echo "$unformatted"
  exit 1
fi
go test -race -count=1 ./...
go vet ./...
