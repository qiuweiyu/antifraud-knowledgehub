#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/../backend"
go run ./cmd/server --seed-only
