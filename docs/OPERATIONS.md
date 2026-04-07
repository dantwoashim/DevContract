# Operations Guide

EnvSync's relay is designed for small-team supportability. This guide documents what to watch, what the relay guarantees, and how to recover when something goes wrong.

## What the Relay Stores

- Team membership documents in Workers KV
- Invite documents in Workers KV
- Encrypted relay blobs and blob metadata in Workers KV
- Pending-queue coordination and per-team counters in Durable Objects
- Rate-limit counters in a dedicated Durable Object

The relay is not the source of truth for plaintext secrets. It stores only encrypted blob payloads and metadata needed to deliver them.

## Health Endpoints

- `GET /health` returns basic service metadata
- `GET /health/live` is a shallow liveness probe
- `GET /health/ready` checks KV reachability plus both Durable Object coordinators

`/health/ready` should be the deployment readiness check. It validates:

- KV access
- team coordinator availability
- rate-limit coordinator availability

## Team Metrics

Authenticated team members can inspect relay-side counters at:

```text
GET /teams/:team/metrics
```

The response includes:

- `member_count`
- `pending_count`
- `pending_by_recipient`
- `uploads_today`
- `event_totals`
- `events_today`
- `recorded_at`

Current counters include:

- `invite.created`
- `invite.consumed`
- `invite.joined`
- `invite.join_failed`
- `team.member_upserted`
- `team.member_removed`
- `team.member_rotated`
- `team.rotate_failed`
- `relay.blob_stored`
- `relay.blob_downloaded`
- `relay.blob_deleted`
- `relay.pending_reconciled`

## Structured Logs

Every request emits a structured completion log with:

- `request_id`
- `method`
- `path`
- `status`
- `duration_ms`
- authenticated `fingerprint` when available

Lifecycle events also emit structured JSON entries with team and actor fields. Search for:

- `invite.*`
- `team.*`
- `relay.*`
- `request.complete`
- `request.unhandled_error`
- `auth.verification_failed`
- `rate_limit.backend_unavailable`

## Alerting Recommendations

EnvSync does not create hosted dashboards from inside this repo. The relay now exposes the fields needed to wire them up quickly.

Recommended alerts:

- sustained `5xx` rate above baseline
- sustained `auth.verification_failed`
- repeated `invite.join_failed`
- repeated `team.rotate_failed`
- `pending_count` growth without matching `relay.blob_deleted`
- repeated `rate_limit.backend_unavailable`
- readiness failures on `/health/ready`

## Backup and Recovery Expectations

Be explicit about the relay's recovery model:

- Team membership and invite state can be restored from KV backups or deployment exports.
- Pending queues and daily counters live in Durable Objects and should be treated as reconstructible operational state.
- If pending queue state is lost, ask senders to re-run `envsync push`.
- Delivered secrets are not retroactively revoked. Revocation blocks future delivery only.

Recovery checklist:

1. Restore the Worker and bindings.
2. Restore KV namespaces containing team, invite, and blob metadata.
3. Bring Durable Objects back online.
4. Verify `/health/ready`.
5. Inspect `/teams/:team/metrics` for queue depth and recent activity.
6. Ask affected users to re-push if pending delivery state was lost.

## Operator Verification

Before a rollout:

1. `npm test` in `relay`
2. `go test ./...`
3. `go run . doctor --skip-relay`
4. confirm `/health/ready`
5. pull a known test blob through the relay path

## Support Boundaries

EnvSync is supportable for small teams when:

- membership changes are visible through metrics and logs
- queue growth is monitored
- readiness is tied to real dependencies
- operators know that replaying a push is the recovery path for lost pending state
