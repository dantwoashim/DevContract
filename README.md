# DevContract

DevContract is an open-source repo-first tool for developer onboarding, local setup contracts, and encrypted `.env` sharing.

It is built for teams that still onboard people through stale docs, copied `.env` files, chat messages, and tribal knowledge. DevContract lets the repository describe how local development should work, helps teammates receive shared config more safely, and keeps local revision history encrypted on each machine.

## Why It Exists

Most teams do local setup with some messy mix of:

- README steps that drift over time
- copied `.env` files in chat or DMs
- hand-written onboarding checklists
- "ask someone on the team" as the real setup process

DevContract tries to replace that with one repo-owned contract for setup, health checks, and shared local config workflows.

## What Makes It Different

- The repo can declare a local setup contract in `.devcontract/contract.yaml`
- The CLI can bootstrap, validate, and run that contract
- Shared `.env` updates can move directly between trusted machines or through an encrypted relay fallback
- Local history and backups stay encrypted on the developer machine
- It is designed for development environments, not production secret injection

## Who It Is For

- small engineering teams with painful onboarding
- solo builders managing more than one machine
- repos that need repeatable local setup, not just secret storage
- teams that want something lighter than a full hosted secrets platform

## Who It Is Not For

- production runtime secrets management
- enterprise compliance-heavy environments
- teams that only need a hosted secret dashboard and nothing else

## What It Does Today

- bootstrap a repository-defined local setup flow
- validate runtimes, services, contract health, and sensitive text surfaces
- invite and join trusted project members
- sync `.env` changes between trusted machines
- keep encrypted local revision and backup history
- support scoped relay pull workflows for CI or service principals

## What Is Still Experimental

- generated assistant/editor instruction files and MCP config
- some extension surfaces
- relay limits messaging

DevContract is for development environments. It is not a production secrets manager or a hosted control plane by itself.

## Portfolio docs

- [Case study](docs/CASE_STUDY.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Threat model](docs/THREAT_MODEL.md)
- [Self-hosting](docs/SELF_HOSTING.md)

## Quick Start

### Install from GitHub Releases

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/dantwoashim/DevContract/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/dantwoashim/DevContract/main/scripts/install.ps1 | iex
```

If a binary release has not been published yet, the installer falls back to building from source when Go is installed.

### Build from Source

```bash
git clone https://github.com/dantwoashim/DevContract.git
cd DevContract
go build -o devcontract ./
```

For contributors and auditors, the repository includes a devcontainer and a canonical `make verify` path using Go `1.25.8` and Node `22`.

### First Run

In your own repository:

```bash
devcontract init
devcontract bootstrap
devcontract doctor
```

Invite a teammate:

```bash
devcontract invite teammate
```

Sync changes:

```bash
devcontract push
devcontract pull
```

## The Main Idea

In a typical repo, you can:

1. define local setup in `.devcontract/contract.yaml`
2. run `devcontract init`
3. run `devcontract bootstrap`
4. run `devcontract doctor`
5. share `.env` updates with `devcontract push` and `devcontract pull`

## Example Workflow

Here is a simple example of the intended flow:

```bash
devcontract init
devcontract bootstrap
devcontract doctor
devcontract invite teammate
devcontract push
devcontract pull
```

The goal is to make local setup repeatable, easier to audit, and less dependent on ad hoc handoffs.

## Core Commands

- `devcontract init`: create local identity state and scaffold a starter contract in your repository
- `devcontract bootstrap`: run the repository setup contract after trust review
- `devcontract doctor`: check local prerequisites and repository health
- `devcontract guard scan`: scan repo text surfaces for likely secret leaks
- `devcontract invite` / `devcontract join`: manage human project membership
- `devcontract limits`: inspect relay-side limits and usage configured by the current deployment
- `devcontract service-key`: register scoped relay machine identities
- `devcontract push` / `devcontract pull`: exchange encrypted `.env` state
- `devcontract backup`, `restore`, `rollback`: inspect and recover local history

## How It Works

- Each repository can define its local setup contract in `.devcontract/contract.yaml`.
- Human identity comes from an Ed25519 SSH key.
- LAN sync uses direct peer transport when a trusted peer is reachable.
- Relay sync uploads a per-recipient encrypted blob when direct delivery is unavailable.
- Revision and backup history stays local and encrypted at rest.

More detail:

- [Architecture](docs/ARCHITECTURE.md)
- [Contract Reference](docs/CONTRACT.md)
- [Threat Model](docs/THREAT_MODEL.md)
- [Self-Hosting](docs/SELF_HOSTING.md)
- [FAQ](docs/FAQ.md)
- [OSS Scope](docs/OSS_SCOPE.md)
- [Operations](docs/OPERATIONS.md)
- [Releases](docs/RELEASES.md)

## Security Summary

- request authentication uses Ed25519 signatures
- peer-to-peer sync uses a secure transport
- relay payloads are encrypted before upload
- relay metadata is still visible to the relay operator
- local machine compromise still defeats local secrecy

See [SECURITY.md](SECURITY.md) for reporting guidance and the current security model.

## Relay and Self-Hosting

The repository includes the relay source in [relay](relay). You can self-host it on Cloudflare Workers or point the CLI at your own relay deployment. The default public relay URL in the codebase is just a default endpoint, not a promise of hosted service guarantees.

## VS Code Extension

The VS Code extension lives in [extension](extension). It shells out to the CLI and is best treated as a companion interface, not a separate implementation.

## Examples

- [examples/devcontract.example.toml](examples/devcontract.example.toml)
- [examples/demo/.env.example](examples/demo/.env.example)
- [examples/contracts](examples/contracts)

## License

[MIT](LICENSE)
