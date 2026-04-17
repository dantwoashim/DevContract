import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import type { UnstableDevWorker } from 'wrangler';
import { createIdentity, registerMember, signedFetch, startTestWorker, transportFingerprint, transportKey } from './helpers';

let worker: UnstableDevWorker;

beforeAll(async () => {
    worker = await startTestWorker();
});

afterAll(async () => {
    await worker?.stop();
}, 30_000);

const TEAM_ID = `test-team-relay-${Date.now()}`;
const BLOB_ID = `blob-${Date.now()}`;
const ENCRYPTED_BODY = Buffer.from('ENCRYPTED_DATA_HERE');

describe('Relay Blob Operations', () => {
    let sender: Awaited<ReturnType<typeof createIdentity>>;
    let recipient: Awaited<ReturnType<typeof createIdentity>>;
    let servicePrincipal: Awaited<ReturnType<typeof createIdentity>>;

    beforeAll(async () => {
        sender = await createIdentity('relay-sender');
        recipient = await createIdentity('relay-recipient');
        servicePrincipal = await createIdentity('relay-ci');

        expect((await registerMember(worker, sender, TEAM_ID, 'alice', transportKey(11), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, sender, `/teams/${TEAM_ID}/members/bob`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: recipient.fingerprint,
                public_key: recipient.publicKeyB64,
                transport_public_key: transportKey(22),
                transport_fingerprint: transportFingerprint(transportKey(22)),
                role: 'member',
            }),
        })).status).toBe(200);
        expect((await signedFetch(worker, sender, `/teams/${TEAM_ID}/members/ci`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: servicePrincipal.fingerprint,
                public_key: servicePrincipal.publicKeyB64,
                transport_public_key: transportKey(44),
                transport_fingerprint: transportFingerprint(transportKey(44)),
                role: 'member',
                principal_type: 'service_principal',
                scopes: ['relay.pull', 'member.read'],
            }),
        })).status).toBe(200);
    });

    it('should upload a blob', async () => {
        const res = await signedFetch(worker, sender, `/relay/${TEAM_ID}/${BLOB_ID}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/octet-stream',
                'X-EnvSync-Sender': sender.fingerprint,
                'X-EnvSync-Recipient': recipient.fingerprint,
                'X-EnvSync-EphemeralKey': transportKey(33),
                'X-EnvSync-Filename': '.env',
                'X-EnvSync-Signature': Buffer.from('test-signature').toString('base64'),
            },
            body: ENCRYPTED_BODY,
        });
        expect(res.status).toBe(201);
    });

    it('should list pending blobs for the recipient', async () => {
        const res = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/pending?for=${encodeURIComponent(recipient.fingerprint)}`);
        expect(res.status).toBe(200);
        const data = await res.json() as { pending: Array<{ blob_id: string }> };
        expect(data.pending.some((blob) => blob.blob_id === BLOB_ID)).toBe(true);
    });

    it('should download a blob as the intended recipient', async () => {
        const res = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/${BLOB_ID}`);
        expect(res.status).toBe(200);
        expect(await res.text()).toBe(ENCRYPTED_BODY.toString());
        expect(res.headers.get('X-EnvSync-Sender')).toBe(sender.fingerprint);
    });

    it('should delete a blob as the intended recipient', async () => {
        const res = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/${BLOB_ID}`, {
            method: 'DELETE',
        });
        expect(res.status).toBe(200);
    });

    it('should return 404 for deleted blob', async () => {
        const res = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/${BLOB_ID}`);
        expect(res.status).toBe(404);
    });

    it('should expose queue and blob counters via team metrics', async () => {
        const res = await signedFetch(worker, sender, `/teams/${TEAM_ID}/metrics`);
        expect(res.status).toBe(200);
        const data = await res.json() as {
            pending_count: number;
            uploads_today: number;
            event_totals: Record<string, number>;
        };
        expect(data.pending_count).toBe(0);
        expect(data.uploads_today).toBeGreaterThanOrEqual(1);
        expect(data.event_totals['relay.blob_stored']).toBeGreaterThanOrEqual(1);
        expect(data.event_totals['relay.blob_downloaded']).toBeGreaterThanOrEqual(1);
        expect(data.event_totals['relay.blob_deleted']).toBeGreaterThanOrEqual(1);
    });

    it('should report truthful relay limits and usage from coordinator-backed metrics', async () => {
        const res = await signedFetch(worker, sender, `/teams/${TEAM_ID}/limits`);
        expect(res.status).toBe(200);
        const data = await res.json() as {
            metering_source: string;
            tier: string;
            usage: {
                member_records: number;
                human_members: number;
                service_principals: number;
                blobs_today: number;
            };
        };
        expect(data.metering_source).toBe('team_coordinator');
        expect(data.tier).toBe('free');
        expect(data.usage.member_records).toBe(3);
        expect(data.usage.human_members).toBe(2);
        expect(data.usage.service_principals).toBe(1);
        expect(data.usage.blobs_today).toBeGreaterThanOrEqual(1);
    });

    it('should reject malformed blobs and expose them to owners', async () => {
        const rejectedBlobId = `blob-rejected-${Date.now()}`;
        const upload = await signedFetch(worker, sender, `/relay/${TEAM_ID}/${rejectedBlobId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/octet-stream',
                'X-EnvSync-Sender': sender.fingerprint,
                'X-EnvSync-Recipient': recipient.fingerprint,
                'X-EnvSync-EphemeralKey': transportKey(55),
                'X-EnvSync-Filename': '.env',
                'X-EnvSync-Signature': Buffer.from('test-signature').toString('base64'),
            },
            body: Buffer.from('BROKEN_BLOB'),
        });
        expect(upload.status).toBe(201);

        const reject = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/${rejectedBlobId}/reject`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ reason: 'signature mismatch' }),
        });
        expect(reject.status).toBe(200);

        const pending = await signedFetch(worker, recipient, `/relay/${TEAM_ID}/pending?for=${encodeURIComponent(recipient.fingerprint)}`);
        expect(pending.status).toBe(200);
        const pendingData = await pending.json() as { pending: Array<{ blob_id: string }> };
        expect(pendingData.pending.some((blob) => blob.blob_id === rejectedBlobId)).toBe(false);

        const rejected = await signedFetch(worker, sender, `/relay/${TEAM_ID}/rejected`);
        expect(rejected.status).toBe(200);
        const rejectedData = await rejected.json() as {
            rejected: Array<{ blob_id: string; status: string; failure_reason: string }>;
        };
        expect(rejectedData.rejected.some((blob) => blob.blob_id === rejectedBlobId && blob.status === 'rejected_client' && blob.failure_reason === 'signature mismatch')).toBe(true);
    });

    it('should block service principals from relay uploads without relay.push scope', async () => {
        const res = await signedFetch(worker, servicePrincipal, `/relay/${TEAM_ID}/blob-service-${Date.now()}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/octet-stream',
                'X-EnvSync-Sender': servicePrincipal.fingerprint,
                'X-EnvSync-Recipient': recipient.fingerprint,
                'X-EnvSync-EphemeralKey': transportKey(66),
                'X-EnvSync-Filename': '.env',
                'X-EnvSync-Signature': Buffer.from('test-signature').toString('base64'),
            },
            body: Buffer.from('SERVICE_PRINCIPAL_UPLOAD'),
        });
        expect(res.status).toBe(403);
    });

    it('should fail closed when the global rate-limit coordinator is unavailable', async () => {
        const res = await signedFetch(worker, sender, `/relay/${TEAM_ID}/pending?for=${encodeURIComponent(sender.fingerprint)}`, {
            headers: {
                'X-EnvSync-Test-RateLimit-Failure': 'global',
            },
        });
        expect(res.status).toBe(503);
        const data = await res.json() as { error: string };
        expect(data.error).toBe('service_unavailable');
    });

    it('should fail closed when the upload quota coordinator is unavailable', async () => {
        const blobId = `blob-degraded-${Date.now()}`;
        const res = await signedFetch(worker, sender, `/relay/${TEAM_ID}/${blobId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/octet-stream',
                'X-EnvSync-Sender': sender.fingerprint,
                'X-EnvSync-Recipient': recipient.fingerprint,
                'X-EnvSync-EphemeralKey': transportKey(77),
                'X-EnvSync-Filename': '.env',
                'X-EnvSync-Signature': Buffer.from('test-signature').toString('base64'),
                'X-EnvSync-Test-RateLimit-Failure': 'team',
            },
            body: Buffer.from('FAIL_CLOSED_UPLOAD'),
        });
        expect(res.status).toBe(503);
        const data = await res.json() as { error: string };
        expect(data.error).toBe('service_unavailable');
    });
});
