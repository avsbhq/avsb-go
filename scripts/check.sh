#!/usr/bin/env bash
#
# Local gate for the A vs B Go SDK: format, vet, and test every module.
#
# CI minutes are exhausted for this project, so this script IS the enforcement
# path. Run it before every commit that touches packages/avsb-go.
#
#   ./scripts/check.sh          # format check, vet, test
#   ./scripts/check.sh --fix    # rewrite formatting, then vet and test
#   ./scripts/check.sh --tidy   # also run `go mod tidy` (needs the network)
#
# The submodules exist so the root SDK carries no dependencies (the framework
# glue, the OpenFeature provider and the Redis, DynamoDB and Postgres sticky
# stores each live in their own module), which means
# `go test ./...` from the root does NOT reach them: Go stops at a nested go.mod.
# Each one is entered explicitly below. Adding a module here is the only step
# needed when a new framework submodule lands.

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

MODULES=(
  "."
  "middleware/ginmw"
  "middleware/echomw"
  "middleware/fibermw"
  "openfeature"
  "sticky/redis"
  "sticky/dynamodb"
  "sticky/postgres"
)

# The floor every module declares. A tidy run on a newer toolchain must not
# raise it, and a dependency that needs a newer Go is pinned below that release.
GO_FLOOR="1.22"

FIX=0
TIDY=0
for arg in "$@"; do
  case "$arg" in
    --fix) FIX=1 ;;
    --tidy) TIDY=1 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

failed=0

for module in "${MODULES[@]}"; do
  echo ""
  echo "=== ${module} ==="
  cd "${ROOT}/${module}"

  if [[ "${TIDY}" -eq 1 ]]; then
    echo "--- go mod tidy"
    go mod tidy -go="${GO_FLOOR}" || failed=1
  fi

  directive="$(awk '$1 == "go" { print $2; exit }' go.mod)"
  if [[ "${directive}" != "${GO_FLOOR}" ]]; then
    echo "go.mod declares go ${directive}, expected ${GO_FLOOR}" >&2
    failed=1
  fi

  echo "--- gofmt"
  if [[ "${FIX}" -eq 1 ]]; then
    gofmt -w .
  else
    # A parse error makes gofmt exit non-zero; record it instead of letting
    # set -e abort the run before the other modules are checked.
    if ! unformatted="$(gofmt -l . 2>&1)"; then
      echo "${unformatted}" >&2
      failed=1
    elif [[ -n "${unformatted}" ]]; then
      echo "not gofmt-clean:" >&2
      echo "${unformatted}" >&2
      echo "run ./scripts/check.sh --fix" >&2
      failed=1
    fi
  fi

  echo "--- go vet"
  go vet ./... || failed=1

  echo "--- go test"
  go test ./... || failed=1
done

cd "${ROOT}"

# The root module promises zero dependencies. A require line means something
# that needs a third-party package was added to it instead of to a submodule.
if grep -q '^require' go.mod; then
  echo "" >&2
  echo "the root go.mod has a require block; move that code into a submodule" >&2
  failed=1
fi
if [[ -f go.sum ]]; then
  echo "the root module has a go.sum; it should have no dependencies to sum" >&2
  failed=1
fi

echo ""
if [[ "${failed}" -ne 0 ]]; then
  echo "FAILED"
  exit 1
fi
echo "OK"
