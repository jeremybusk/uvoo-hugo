#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT/editor/web"
ALLOWED_GO="${ALLOWED_GO:-Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,BlueOak-1.0.0}"
ALLOWED_NPM="${ALLOWED_NPM:-Apache-2.0;MIT;BSD-2-Clause;BSD-3-Clause;ISC;BlueOak-1.0.0;0BSD;Python-2.0;CC-BY-4.0;W3C-20150513;(MPL-2.0 OR Apache-2.0)}"
ALLOWED_LOCK_PATTERN="${ALLOWED_LOCK_PATTERN:-^(Apache-2.0|MIT|BSD-2-Clause|BSD-3-Clause|ISC|BlueOak-1.0.0|0BSD|Python-2.0|CC-BY-4.0|W3C-20150513|\\(MPL-2.0 OR Apache-2.0\\))$}"
INCLUDE_DEV_LICENSES="${INCLUDE_DEV_LICENSES:-0}"
REQUIRE_LICENSE_TOOLS="${REQUIRE_LICENSE_TOOLS:-0}"
status=0

fail() {
  echo "license-check: $*" >&2
  status=1
}

echo "checking project license metadata"
grep -q "Apache License" "$ROOT/LICENSE" || fail "LICENSE is not Apache-2.0 text"
[ -f "$ROOT/NOTICE" ] || fail "NOTICE file is missing"
grep -q "license: Apache-2.0" "$ROOT/.goreleaser.yaml" || fail ".goreleaser.yaml package license is not Apache-2.0"
if command -v jq >/dev/null 2>&1; then
  package_license="$(jq -r '.license // ""' "$WEB_DIR/package.json")"
  [ "$package_license" = "Apache-2.0" ] || fail "editor/web/package.json license is not Apache-2.0"
else
  echo "skipping package.json metadata check: install jq" >&2
fi

echo "checking source SPDX markers"
missing_spdx="$(
  cd "$ROOT"
  git ls-files \
    '*.go' '*.sh' '*.ts' '*.tsx' '*.css' \
    '.goreleaser.yaml' '.github/workflows/*.yml' '.github/workflows/*.yaml' \
    'Dockerfile' 'Makefile' |
  while IFS= read -r file; do
    if ! sed -n '1,20p' "$file" | grep -q 'SPDX-License-Identifier: Apache-2.0'; then
      printf '%s\n' "$file"
    fi
  done
)"
if [ -n "$missing_spdx" ]; then
  fail "tracked source/build files missing SPDX-License-Identifier: Apache-2.0"
  printf '%s\n' "$missing_spdx" >&2
fi

echo "checking Go module licenses"
if command -v go-licenses >/dev/null 2>&1; then
  if ! go-licenses check "$ROOT/..." --allowed_licenses="$ALLOWED_GO"; then
    status=1
  fi
else
  if [ "$REQUIRE_LICENSE_TOOLS" = "1" ]; then
    fail "missing Go license scanner: install with 'go install github.com/google/go-licenses@latest'"
  else
    echo "skipping Go license scan: install with 'go install github.com/google/go-licenses@latest'" >&2
  fi
fi

echo "checking npm package licenses"
if command -v jq >/dev/null 2>&1 && [ -f "$WEB_DIR/package-lock.json" ]; then
  lock_issues="$(
    jq -r --arg pattern "$ALLOWED_LOCK_PATTERN" '
      .packages
      | to_entries[]
      | select(.key != "")
      | select(env.INCLUDE_DEV_LICENSES == "1" or (.value.dev != true))
      | select(.value.license and (.value.license | test($pattern) | not))
      | "\(.key)\t\(.value.version // "")\t\(.value.license)"
    ' "$WEB_DIR/package-lock.json"
  )"
  if [ -n "$lock_issues" ]; then
    fail "package-lock.json contains licenses outside the allow-list"
    printf '%s\n' "$lock_issues" >&2
  fi
else
  echo "skipping package-lock license check: install jq" >&2
fi

if [ -x "$WEB_DIR/node_modules/.bin/license-checker-rseidelsohn" ]; then
  (
    cd "$WEB_DIR"
    ./node_modules/.bin/license-checker-rseidelsohn --production --excludePrivatePackages --summary --onlyAllow "$ALLOWED_NPM"
  ) || status=1
elif command -v license-checker-rseidelsohn >/dev/null 2>&1; then
  (
    cd "$WEB_DIR"
    license-checker-rseidelsohn --production --excludePrivatePackages --summary --onlyAllow "$ALLOWED_NPM"
  ) || status=1
elif [ -x "$WEB_DIR/node_modules/.bin/license-checker" ]; then
  (
    cd "$WEB_DIR"
    ./node_modules/.bin/license-checker --production --excludePrivatePackages --summary --onlyAllow "$ALLOWED_NPM"
  ) || status=1
elif command -v license-checker >/dev/null 2>&1; then
  (
    cd "$WEB_DIR"
    license-checker --production --excludePrivatePackages --summary --onlyAllow "$ALLOWED_NPM"
  ) || status=1
else
  if [ "$REQUIRE_LICENSE_TOOLS" = "1" ]; then
    fail "missing npm license scanner: run 'npm ci' in editor/web"
  else
    echo "skipping npm license scan: run 'npm ci' in editor/web" >&2
  fi
fi

exit "$status"
