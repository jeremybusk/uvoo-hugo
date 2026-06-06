#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

scancode -cl \
  --ignore ".git" \
  --ignore "editor/web/node_modules" \
  --ignore "editor/web/dist" \
  --ignore "hugo_website_demo/public" \
  --ignore "hugo_website_demo/resources" \
  --ignore "dist" \
  --ignore "bin" \
  --ignore "scancode-report.json" \
  --json-pp scancode-source-report.json ./
