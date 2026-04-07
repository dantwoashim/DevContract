# Self-Hosting

EnvSync can use a self-hosted relay instead of the default hosted endpoint.

## What You Are Hosting

The relay is a Cloudflare Worker application in `relay/`. It provides:

- invite issuance and redemption
- membership and service principal management
- encrypted blob upload and download
- relay-side audit and queue state

It does not decrypt payloads for clients.

## Local Development

```bash
cd relay
npm ci
npx wrangler dev --local --test-scheduled --port 8787
```

The default local health endpoint is:

```text
http://127.0.0.1:8787/health
```

## Required Bindings

The worker expects:

- one KV namespace bound as `ENVSYNC_DATA`
- one Durable Object binding named `TEAM_COORDINATOR`
- one Durable Object binding named `RATE_LIMIT_COORDINATOR`

The checked-in `wrangler.toml` uses placeholder IDs. Replace them in your deployment environment.

## Important Variables

From `relay/wrangler.toml`:

- `ENVIRONMENT`
- `MAX_BLOB_SIZE`
- `INVITE_TTL_HOURS`
- `BLOB_TTL_HOURS`
- optional `CORS_ALLOW_ORIGIN`
- optional `BILLING_ENABLED`

## Deploy

```bash
cd relay
npm ci
npx wrangler deploy
```

Point the CLI or project config at your relay URL:

```toml
relay_url = "https://your-relay.example.com"
```

## Caveats

- The checked-in relay is suitable for self-hosting and controlled deployments, not as a turnkey multi-tenant SaaS control plane.
- You are responsible for Cloudflare account limits, logs, incident response, and namespace lifecycle.
- Relay metadata is visible to the relay operator even though payload contents remain encrypted.
