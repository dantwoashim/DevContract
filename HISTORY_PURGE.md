# History Purge Guide

Use this only if previously committed local state or generated artifacts should be removed from repository history.

## Paths worth checking

- `.env.local`
- `.env.agent`
- `.envsync.toml`
- `.envsync/`
- `.cursor/`
- `.claude/`
- `.vscode/mcp.json`
- `mcp.json`
- `WORKSPACE.md`
- `.gocache/`
- `envsync.exe`
- `extension/out/`
- `extension/*.vsix`
- `extension/node_modules/`
- `relay/node_modules/`
- `relay/.wrangler/`

## Recommended tool

Use [`git filter-repo`](https://github.com/newren/git-filter-repo) on a fresh clone.

## Example

```bash
git clone --mirror https://github.com/dantwoashim/Env_sync.git envsync-history-clean.git
cd envsync-history-clean.git

git filter-repo \
  --path .env.local --invert-paths \
  --path .env.agent --invert-paths \
  --path .envsync.toml --invert-paths \
  --path .envsync --invert-paths \
  --path .cursor --invert-paths \
  --path .claude --invert-paths \
  --path .vscode/mcp.json --invert-paths \
  --path mcp.json --invert-paths \
  --path WORKSPACE.md --invert-paths \
  --path .gocache --invert-paths \
  --path envsync.exe --invert-paths \
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
