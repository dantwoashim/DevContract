# History Purge Guide

Use this only if previously committed local state or generated artifacts should be removed from repository history.

## Paths worth checking

- `.env.local`
- `.env.agent`
- `.devcontract.toml`
- `.devcontract/`
- `.cursor/`
- `.claude/`
- `.vscode/mcp.json`
- `mcp.json`
- `WORKSPACE.md`
- `.gocache/`
- `devcontract.exe`
- `extension/out/`
- `extension/*.vsix`
- `extension/node_modules/`
- `relay/node_modules/`
- `relay/.wrangler/`

## Recommended tool

Use [`git filter-repo`](https://github.com/newren/git-filter-repo) on a fresh clone.

## Example

```bash
git clone --mirror https://github.com/dantwoashim/DevContract.git devcontract-history-clean.git
cd devcontract-history-clean.git

git filter-repo \
  --path .env.local --invert-paths \
  --path .env.agent --invert-paths \
  --path .devcontract.toml --invert-paths \
  --path .devcontract --invert-paths \
  --path .cursor --invert-paths \
  --path .claude --invert-paths \
  --path .vscode/mcp.json --invert-paths \
  --path mcp.json --invert-paths \
  --path WORKSPACE.md --invert-paths \
  --path .gocache --invert-paths \
  --path devcontract.exe --invert-paths \
  --path extension/out --invert-paths \
  --path extension/node_modules --invert-paths \
  --path relay/node_modules --invert-paths \
  --path relay/.wrangler --invert-paths
```

Then force-push the rewritten refs only after coordinating with every clone and fork that matters.

## After purging

- rotate any secrets that may have lived in removed files
- invalidate old service keys if they were ever committed
- notify contributors that history changed
