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
# The submodules exist so the root SDK carries no dependencies, which means
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
)

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
    go mod tidy || failed=1
  fi

  echo "--- gofmt"
  if [[ "${FIX}" -eq 1 ]]; then
    gofmt -w .
  else
    unformatted="$(gofmt -l .)"
    if [[ -n "${unformatted}" ]]; then
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

# The build-tagged sticky adapters compile only with their tag, so a plain
# `go vet ./...` never type-checks them and a signature change to
# StickyBucketService can rot them silently. They need their own dependencies,
# which the root module deliberately does not carry, so this is a vet-only pass
# that reports rather than fails.
echo ""
echo "=== build-tagged sticky adapters (advisory) ==="
for tag in redis dynamodb postgres; do
  echo "--- go vet -tags ${tag} ./sticky/..."
  if ! go vet -tags "${tag}" ./sticky/... 2>&1 | sed 's/^/    /'; then
    echo "    (needs the ${tag} client module; add it to a scratch module to type-check)"
  fi
done

echo ""
if [[ "${failed}" -ne 0 ]]; then
  echo "FAILED"
  exit 1
fi
echo "OK"
