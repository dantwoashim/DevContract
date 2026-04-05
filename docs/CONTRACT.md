# Contract Reference

The repo onboarding contract lives at `.envsync/contract.yaml`.

It is the repo-owned source of truth for:

- environment variables
- local runtimes
- local services
- bootstrap steps
- generated onboarding files
- agent instructions
- MCP config templates
- secret guard policies
- runnable workflow targets

## Version

Current supported version:

```yaml
version: 1
```

## Top-Level Schema

```yaml
version: 1
project:
  slug: my-ai-app
  name: My AI App
  summary: Short product or repo description
env:
  required: []
  optional: []
  public: []
runtimes: []
services: []
bootstrap:
  steps: []
  outputs: []
doctor:
  checks: []
agents: {}
mcp:
  servers: []
policies:
  redact_paths: []
  block_patterns: []
run:
  default: dev
  targets: {}
```

## `project`

```yaml
project:
  slug: my-ai-app
  name: My AI App
  summary: Agent-safe repo onboarding contract
```

- `slug` is required
- `name` and `summary` are optional but strongly recommended

## `env`

Supported sources:

- `shared`
- `developer-local`
- `ci-only`
- `manual`

Example:

```yaml
env:
  required:
    - name: OPENAI_API_KEY
      source: shared
      description: Shared provider key for local development
  optional:
    - name: LANGSMITH_API_KEY
      source: developer-local
  public:
    - NEXT_PUBLIC_API_BASE_URL
```

Rules:

- env names must be uppercase snake_case
- a variable may appear only once across `required`, `optional`, and `public`

## `runtimes`

Scalar form:

```yaml
runtimes:
  - node
  - pnpm
  - python
```

Expanded form:

```yaml
runtimes:
  - name: node
    binary: node
    version_args: ["--version"]
    required: true
```

Supported common defaults include:

- `node`
- `npm`
- `pnpm`
- `python`
- `uv`
- `go`
- `docker`

## `services`

Use this for local services the repo expects during development.

```yaml
services:
  - name: web
    host: 127.0.0.1
    port: 3000
    required: false
    start: npm run dev
    description: Local web app
```

`doctor` checks whether the service is reachable. If `required: true`, a missing service is blocking.

## `bootstrap`

Bootstrap steps run during `envsync bootstrap`.

Scalar form:

```yaml
bootstrap:
  steps:
    - pnpm install
```

Expanded form:

```yaml
bootstrap:
  steps:
    - name: install
      run: pnpm install
      shell: ""
      optional: false
      description: Install repo dependencies
```

Outputs define local files that bootstrap may create.

```yaml
bootstrap:
  outputs:
    - path: .env.local
      kind: env
      gitignore: true
      header: Developer-local environment overrides
```

Current output behavior:

- `kind: env` creates a starter file with commented placeholders for known env vars
- `gitignore: true` appends the path to `.gitignore` if needed

## `doctor.checks`

Contract-defined doctor checks are optional.

```yaml
doctor:
  checks:
    - name: contract-file
      type: file_exists
      target: .envsync/contract.yaml
      required: true
    - name: codex-mcp
      type: json_valid
      target: mcp.json
    - name: local-web
      type: tcp
      target: 127.0.0.1:3000
```

Supported types:

- `file_exists`: repo-relative path must exist
- `json_valid`: repo-relative file must exist and contain valid JSON
- `tcp`: host:port must accept a TCP connection

## `agents`

Supported agent keys:

- `codex`
- `copilot`
- `cursor`
- `claude`

Example:

```yaml
agents:
  codex:
    output: AGENTS.md
    mcp_output: mcp.json
    header: Codex instructions
    instructions:
      - Run envsync doctor before major edits.
      - Never inline secrets into prompts or MCP config.
  copilot:
    output: .github/copilot-instructions.md
    mcp_output: .vscode/mcp.json
```

Recommended output targets:

- Codex: `AGENTS.md`
- Copilot: `.github/copilot-instructions.md`
- Cursor: `.cursor/rules/envsync.mdc`
- Claude: `.claude/ENVSYNC.md`

`envsync agent install` writes these files from the contract.

## `mcp`

MCP templates are generated as JSON with environment placeholders only.

```yaml
mcp:
  servers:
    - name: repo-docs
      command: node
      args:
        - scripts/mcp-docs.js
      env:
        - OPENAI_API_KEY
        - GITHUB_TOKEN
      description: Local MCP server for repo docs
```

Generated JSON uses:

```json
{
  "servers": {
    "repo-docs": {
      "command": "node",
      "args": ["scripts/mcp-docs.js"],
      "env": {
        "OPENAI_API_KEY": "${OPENAI_API_KEY}",
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

Raw secrets must never appear in generated MCP files.

## `policies`

```yaml
policies:
  redact_paths:
    - AGENTS.md
    - .github/copilot-instructions.md
    - .cursor
    - .claude
    - prompts
  block_patterns:
    - ENVSYNC_SERVICE_KEY=[A-Za-z0-9+/=]{32,}
```

Meaning:

- `redact_paths` identifies sensitive agent-facing or shareable paths
- `block_patterns` adds custom regex rules for `guard scan`

Guard defaults already look for:

- OpenAI-style API keys
- Anthropic keys
- GitHub PATs
- bearer tokens
- suspicious inline secret assignments

## `run`

Run targets power `envsync run`.

```yaml
run:
  default: dev
  targets:
    dev:
      command: pnpm dev
      description: Start the web app
    test:
      command: pnpm test
      description: Run tests
```

## Command Behavior

### `envsync init`

- creates `.envsync.toml` if missing
- creates `.envsync/contract.yaml` if missing

### `envsync bootstrap`

- validates the contract
- prepares outputs
- checks runtimes
- optionally pulls shared secrets
- runs bootstrap steps

### `envsync agent install --all`

- renders agent files from `agents`
- renders MCP JSON from `mcp.servers`

### `envsync doctor`

- validates contract correctness
- checks env availability, runtimes, services, contract-defined doctor checks, agent files, MCP files, and guard state

### `envsync guard scan`

- scans agent-facing and generated files by default
- use `--staged` to inspect staged changes
- use `--path` to target additional files such as `.env`

## Example Contracts

Starter examples live in [examples/contracts](../examples/contracts):

- `nextjs-openai.yaml`
- `fastapi-openai.yaml`
- `vercel-ai-sdk.yaml`
- `langgraph-python.yaml`
