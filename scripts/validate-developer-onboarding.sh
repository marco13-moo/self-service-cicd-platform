#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root/control-plane"
export GOCACHE="${GOCACHE:-/tmp/self-service-cicd-go-cache}"
go test ./internal/api -run 'TestGoldenPathCatalogAndDiagnostics|TestTenant' -count=1
go test ./cmd/platformctl -count=1
