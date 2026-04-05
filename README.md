# EnvSync

`EnvSync` is the current working name for an agent-safe repo onboarding CLI.

The product is no longer positioned as "just `.env` sync". The real job is:

`git clone` -> one bootstrap path -> working local app -> safe agent config -> no secret leaks in prompts, MCP config, or git history.

This repository's source of truth is [docs/PROJECT_BIBLE.md](docs/PROJECT_BIBLE.md).

## What It Does

- defines a repo-owned onboarding contract in `.envsync/contract.yaml`
- bootstraps local setup with `envsync bootstrap`
- validates the repo, runtimes, services, contract-defined doctor checks, agent files, and MCP config with `envsync doctor`
- generates safe agent instructions and MCP templates with `envsync agent install`
- scans agent-facing files for leaked secrets with `envsync guard scan`
- still supports encrypted shared secret sync with `init`, `invite`, `join`, `push`, `pull`, `backup`, and `rollback`

## What It Is Not

- not a general-purpose enterprise secrets manager
- not a Vault, 1Password, or Infisical replacement
- not a production runtime secret injector
- not a dashboard-first product
- not ready to launch publicly under the `EnvSync` name without a rename and clearance pass

## Quick Start

```bash
# Initialize project identity and scaffold the repo contract
envsync init

# Review or edit the repo contract
cat .envsync/contract.yaml

# Pull shared secrets when available, prepare local files, and run setup
envsync bootstrap

# Preview bootstrap or run commands without executing them
envsync bootstrap --dry-run
envsync run --dry-run

# Generate agent instructions and MCP config for your tools
envsync agent install --all

# Validate the repo and scan for agent-facing secret leaks
envsync doctor
envsync guard scan

# Run the repo's default workflow
envsync run
```

## Core Commands

| Command | Purpose |
| --- | --- |
| `envsync init` | Initialize identity, stable project ID, and starter contract |
| `envsync bootstrap` | Prepare local repo state from `.envsync/contract.yaml` |
| `envsync doctor` | Validate repo health, runtimes, services, agent files, MCP config, and EnvSync setup |
| `envsync agent install --all` | Generate `AGENTS.md`, Copilot, Cursor, Claude, and MCP files from the contract |
| `envsync guard scan` | Scan agent-facing files for raw secrets and inline credentials |
| `envsync guard hook install` | Install a pre-commit hook that blocks dangerous staged changes |
| `envsync run [target]` | Run a named workflow target from the contract |
| `envsync invite <label>` | Create an invite for a teammate |
| `envsync join <code>` | Join the project and trust the registered device identity |
| `envsync push` | Send shared `.env` updates to peers |
| `envsync pull` | Pull relay updates, then listen for LAN delivery |
| `envsync backup` | Create an encrypted local backup |
| `envsync rollback` | Restore the latest backup |
| `envsync service-key ...` | Manage CI or automation relay access |

Managed billing and hosted checkout are disabled in this build. `envsync upgrade` reports the current relay entitlement status only.

Passphrase-protected SSH keys are not yet supported for `envsync init`; use a dedicated unencrypted Ed25519 key for EnvSync if needed.

## Repo Contract

Every repo can define its onboarding contract in `.envsync/contract.yaml`.

Minimum schema:

- `version`
- `project.slug`
- `env.required[]`
- `env.optional[]`
- `env.public[]`
- `runtimes[]`
- `services[]`
- `bootstrap.steps[]`
- `bootstrap.outputs[]`
- `agents.<agent>.output`
- `mcp.servers[]`
- `policies.redact_paths[]`
- `policies.block_patterns[]`
- `run.targets[]`

Example:

```yaml
version: 1
project:
  slug: my-ai-app
  name: My AI App
env:
  required:
    - name: OPENAI_API_KEY
      source: shared
runtimes:
  - node
  - pnpm
bootstrap:
  steps:
    - pnpm install
  outputs:
    - path: .env.local
      kind: env
      gitignore: true
agents:
  codex:
    output: AGENTS.md
    mcp_output: mcp.json
  copilot:
    output: .github/copilot-instructions.md
    mcp_output: .vscode/mcp.json
run:
  default: dev
  targets:
    dev:
      command: pnpm dev
```

More detail is in [docs/CONTRACT.md](docs/CONTRACT.md).

## Example Contracts

Starter contract examples live in [examples/contracts](examples/contracts):

- `nextjs-openai.yaml`
- `fastapi-openai.yaml`
- `vercel-ai-sdk.yaml`
- `langgraph-python.yaml`

## Current Architecture

- Go CLI for identity, contracts, bootstrap, doctor, guard, sync, backup, and rollback
- repo-local `.envsync/contract.yaml` as the onboarding contract
- repo-local `.envsync.toml` as the project identity and relay config
- local encrypted backup store under the user data directory
- direct LAN delivery with mDNS plus encrypted relay fallback
- Cloudflare Worker relay with signed requests and per-project membership auth

## Product Direction

The target customer is an AI-heavy dev team with:

- multiple model-provider keys
- MCP servers and repo-scoped agent config
- Copilot, Codex, Cursor, or Claude in daily use
- messy onboarding and real risk of leaking secrets into prompts or config

The long-term value is not "we sync `.env` files". It is:

- one repo contract
- one bootstrap path
- one doctor path
- one safe set of agent files
- one secure way to share local AI dev secrets

## Docs

- [docs/PROJECT_BIBLE.md](docs/PROJECT_BIBLE.md): product, architecture, GTM, pricing, launch gates
- [docs/CONTRACT.md](docs/CONTRACT.md): contract schema and command behavior
- [docs/LAUNCH.md](docs/LAUNCH.md): public-facing positioning and launch checklist

## License

MIT
