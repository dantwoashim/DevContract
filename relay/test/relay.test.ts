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

    beforeAll(async () => {
        sender = await createIdentity('relay-sender');
        recipient = await createIdentity('relay-recipient');

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
});
