# Contract Reference

EnvSync stores repository setup in `.envsync/contract.yaml`.

The contract is the repository-owned description of how local development should work: which variables exist, which runtimes are expected, which services should be available, what bootstrap should do, and which checks the team cares about.

## Version

Current supported version:

```yaml
version: 1
```

## Top-Level Schema

```yaml
version: 1
project:
  slug: payments-api
  name: Payments API
  summary: Shared local setup contract
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
  slug: payments-api
  name: Payments API
  summary: Shared local setup contract
```

- `slug` is required
- `name` and `summary` are optional

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
    - name: DATABASE_URL
      source: shared
      description: Shared database connection string for local development
    - name: SESSION_SECRET
      source: shared
  optional:
    - name: GITHUB_TOKEN
      source: developer-local
  public:
    - PORT
    - NEXT_PUBLIC_API_BASE_URL
```

Rules:

- variable names must be uppercase snake case
- a variable may appear only once across `required`, `optional`, and `public`

## `runtimes`

Scalar form:

```yaml
runtimes:
  - node
  - pnpm
  - go
```

Expanded form:

```yaml
runtimes:
  - name: node
    binary: node
    version_args: ["--version"]
    required: true
```

Common defaults include:

- `node`
- `npm`
- `pnpm`
- `python`
- `uv`
- `go`
- `docker`

## `services`

Use this section for local services the repository expects during development.

```yaml
services:
  - name: web
    host: 127.0.0.1
    port: 3000
    required: false
    start: npm run dev
    description: Local web application
```

`envsync doctor` checks whether the service is reachable. If `required: true`, a missing service is blocking.

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
      optional: false
      description: Install repository dependencies
```

Outputs describe local files bootstrap may create.

```yaml
bootstrap:
  outputs:
    - path: .env.local
      kind: env
      gitignore: true
      header: Developer-local environment overrides
```

Current output behavior:

- `kind: env` creates a starter file with commented placeholders for known variables
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
    - name: workspace-config
      type: json_valid
      target: .vscode/settings.json
    - name: local-web
      type: tcp
      target: 127.0.0.1:3000
```

Supported types:

- `file_exists`: repo-relative path must exist
- `json_valid`: repo-relative file must exist and contain valid JSON
- `tcp`: host:port must accept a TCP connection

## `run`

Use named targets to describe common local workflows.

```yaml
run:
  default: dev
  targets:
    dev:
      command: make dev
      description: Start the default local development workflow
    test:
      command: make test
```

`envsync run` executes the default target when one is defined.

## `policies`

Use policy rules to widen secret scanning beyond the built-in patterns.

```yaml
policies:
  redact_paths:
    - .env.local
    - docs/private
  block_patterns:
    - INTERNAL_AUDIT_SECRET=\\w+
```

- `redact_paths` adds repo-relative locations to secret scanning
- `block_patterns` adds custom regular expressions that should be treated as blocking findings

## Optional Tool Targets

The `agents` and `mcp` sections are optional and should be treated as experimental companion features. This repository does not commit the generated outputs; generate them locally if you need them.

They exist for repositories that want EnvSync to generate instruction files and companion JSON config for supported tools.

Example:

```yaml
agents:
  copilot:
    output: .github/copilot-instructions.md
    mcp_output: .vscode/mcp.json
  cursor:
    output: .cursor/rules/envsync.mdc
    mcp_output: .cursor/mcp.json
mcp:
  servers:
    - name: repo-docs
      command: node
      args:
        - scripts/mcp-docs.js
      env:
        - GITHUB_TOKEN
```

If you use these sections:

- keep secrets in environment variables, not inline JSON
- let EnvSync write the generated files
- include those paths in guard scanning if they are not already covered

## Complete Example

```yaml
version: 1
project:
  slug: next-storefront
  name: Next Storefront
  summary: Local setup contract for the storefront application
env:
  required:
    - name: DATABASE_URL
      source: shared
    - name: SESSION_SECRET
      source: shared
  optional:
    - name: GITHUB_TOKEN
      source: developer-local
  public:
    - NEXT_PUBLIC_API_BASE_URL
runtimes:
  - node
  - pnpm
services:
  - name: web
    host: 127.0.0.1
    port: 3000
    start: pnpm dev
bootstrap:
  steps:
    - pnpm install
  outputs:
    - path: .env.local
      kind: env
      gitignore: true
doctor:
  checks:
    - name: local-web
      type: tcp
      target: 127.0.0.1:3000
run:
  default: dev
  targets:
    dev:
      command: pnpm dev
```

## Related Commands

- `envsync init`: creates a starter contract when one does not already exist
- `envsync bootstrap`: reads the contract and performs local setup
- `envsync doctor`: validates the contract, environment, services, and local state
- `envsync run`: executes named workflow targets from the contract
- `envsync agent install`: generates optional tool-specific files when `agents` is configured
