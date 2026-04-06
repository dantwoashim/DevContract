# EnvSync

EnvSync is a developer tool for one of the messiest parts of software work: getting a repository into a usable state without passing secrets around by hand.

Most teams still do this with a mix of chat threads, stale setup notes, copied `.env` files, and tribal memory. It works until it doesn't. New teammates lose time. Old teammates become the source of truth by accident. Sensitive values end up in the wrong places.

EnvSync gives the repository a contract for local setup and a secure path for sharing development secrets. The contract lives in the repo. Identity comes from existing SSH keys. Shared values are encrypted end to end.

## What EnvSync Does

- Syncs project `.env` files between trusted developers
- Uses SSH keys for identity and project membership
- Stores setup expectations in a repo-owned contract at `.envsync/contract.yaml`
- Bootstraps local development with repeatable setup steps
- Runs health checks for runtimes, services, project metadata, and secret safety
- Creates encrypted local backups before changes are applied
- Supports optional generated instruction files and JSON config for editor and tool integrations

## What EnvSync Is For

EnvSync is built for development environments.

It is a good fit when:

- a repository depends on shared local environment variables
- onboarding requires more than "copy this file and hope for the best"
- teams want setup steps, required services, and environment expectations versioned with the code
- you want peer-to-peer trust and encrypted relay fallback without introducing a full secrets platform

It is not a production secret injector, a hosted vault, or a replacement for infrastructure-focused systems such as Vault, Infisical, or cloud-native secret managers.

## How It Works

1. `envsync init` reads your SSH Ed25519 key, creates local EnvSync config, and scaffolds a starter project contract.
2. `envsync bootstrap` prepares local files, verifies runtimes, and runs the repository's setup steps.
3. `envsync invite` and `envsync join` establish project membership between developers.
4. `envsync push` and `envsync pull` exchange encrypted `.env` changes over LAN when possible and fall back to the relay when needed.

The relay never stores plaintext values. Shared blobs are encrypted client-side for the intended recipient before upload.

## Quick Start

Build from source:

```bash
git clone https://github.com/dantwoashim/Env_sync.git
cd Env_sync
go build -o envsync ./
```

Initialize the current repository:

```bash
./envsync init
```

Review the generated contract:

```bash
cat .envsync/contract.yaml
```

Bootstrap local setup:

```bash
./envsync bootstrap
```

Run health checks:

```bash
./envsync doctor
./envsync guard scan
```

Invite another developer:

```bash
./envsync invite project-member
```

Share updates:

```bash
./envsync push
./envsync pull
```

## Core Commands

| Command | Purpose |
| --- | --- |
| `envsync init` | Initialize local identity and scaffold a starter project contract |
| `envsync bootstrap` | Prepare local outputs and run the repository bootstrap workflow |
| `envsync doctor` | Validate repository setup, local prerequisites, and EnvSync state |
| `envsync guard scan` | Scan instruction files, JSON config, and other sensitive text surfaces for secrets |
| `envsync run [target]` | Run a named workflow target from the contract |
| `envsync invite <label>` | Create a join code for another developer |
| `envsync join <code>` | Join an existing project |
| `envsync push` | Encrypt and send local `.env` updates |
| `envsync pull` | Receive pending updates from trusted project members |
| `envsync backup` | Create an encrypted local backup |
| `envsync restore` | Restore a previous backup |
| `envsync status` | Show current project sync status |
| `envsync service-key ...` | Manage service keys for CI and automation |

## The Project Contract

Every repository can define its local setup contract in `.envsync/contract.yaml`.

That contract can describe:

- required and optional environment variables
- expected runtimes
- local services
- bootstrap steps
- files bootstrap may create
- custom doctor checks
- named workflow targets
- optional generated tool configuration files
- secret scanning policy

Minimal example:

```yaml
version: 1
project:
  slug: payments-api
  name: Payments API
env:
  required:
    - name: DATABASE_URL
      source: shared
    - name: SESSION_SECRET
      source: shared
  public:
    - PORT
runtimes:
  - go
  - node
services:
  - name: api
    host: 127.0.0.1
    port: 8080
    start: make dev
bootstrap:
  steps:
    - make setup
  outputs:
    - path: .env.local
      kind: env
      gitignore: true
run:
  default: dev
  targets:
    dev:
      command: make dev
```

The full reference is in [docs/CONTRACT.md](docs/CONTRACT.md).

## Example Contracts

Starter contracts live in [examples/contracts](examples/contracts):

- `nextjs-web-app.yaml`
- `fastapi-service.yaml`
- `pnpm-web-app.yaml`
- `python-worker.yaml`

## Security Model

- Identity is derived from existing SSH Ed25519 keys
- Project membership is explicit
- Local backups are encrypted at rest
- Relay uploads are encrypted before they leave the client
- Guard scans help catch secrets in instruction files, JSON config, logs, and other text surfaces before they leak

For the full security policy, see [SECURITY.md](SECURITY.md).

## Current Notes

- `envsync init` and other identity-based commands support passphrase-protected Ed25519 keys. In non-interactive environments, provide the passphrase with `ENVSYNC_SSH_KEY_PASSPHRASE`.
- `envsync upgrade` reports entitlement state only; managed checkout and hosted billing are disabled in this build.
- The optional GitHub Action, relay service, and VS Code extension live in this repository alongside the CLI.

## Repository Layout

- [cmd](cmd): CLI commands
- [internal](internal): core packages
- [relay](relay): relay service
- [extension](extension): VS Code extension
- [action](action): GitHub Action

## License

MIT
