#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

tracked_patterns=(
  ".env.local"
  ".env.agent"
  ".devcontract.toml"
  ".devcontract/**"
  ".gocache/**"
  "devcontract"
  "devcontract.exe"
  "extension/*.vsix"
  "extension/out/**"
  "extension/node_modules/**"
  "relay/node_modules/**"
  "relay/.wrangler/**"
  "mcp.json"
  ".vscode/mcp.json"
  ".cursor/**"
  ".claude/**"
  ".github/copilot-instructions.md"
  "WORKSPACE.md"
)

present_paths=(
  ".env.local"
  ".env.agent"
  ".devcontract.toml"
  ".devcontract"
  ".gocache"
  "devcontract"
  "devcontract.exe"
  "extension/out"
  "extension/node_modules"
  "extension/devcontract-vscode-1.0.0.vsix"
  "relay/node_modules"
  "relay/.wrangler"
  "mcp.json"
  ".vscode/mcp.json"
  ".cursor"
  ".claude"
  ".github/copilot-instructions.md"
  "WORKSPACE.md"
)

fail=0

for pattern in "${tracked_patterns[@]}"; do
  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    if [[ -e "$file" ]]; then
      echo "tracked hygiene violation: $file"
      fail=1
    fi
  done < <(git ls-files -- "$pattern")
done

for path in "${present_paths[@]}"; do
  if [[ -e "$path" ]]; then
    echo "working tree hygiene violation: $path"
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo "repo hygiene check failed"
  exit 1
fi

echo "repo hygiene check passed"
