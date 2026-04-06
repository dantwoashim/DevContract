/**
 * Auth middleware tests (self-bootstrapping via wrangler unstable_dev).
 *
 * Tests Ed25519 signature verification:
 *   1. Missing auth header is rejected on protected routes
 *   2. Invalid signature format is rejected
 *   3. Health endpoint does not require auth
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import type { UnstableDevWorker } from 'wrangler';
import { createIdentity, signedFetch, startTestWorker, transportFingerprint, transportKey } from './helpers';

let worker: UnstableDevWorker;

beforeAll(async () => {
    worker = await startTestWorker();
});

afterAll(async () => {
    await worker?.stop();
}, 30_000);

describe('Auth Middleware', () => {
    it('should reject POST /invites without auth header', async () => {
        const res = await worker.fetch(`/invites`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                token_hash: 'test',
                team_id: 'test',
                inviter: 'alice',
                inviter_fingerprint: 'fp',
                invitee: 'bob',
            }),
        });
        expect(res.status).toBe(401);
    });

    it('should reject requests with invalid auth format', async () => {
        const res = await worker.fetch(`/invites`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'InvalidFormat garbage',
            },
            body: JSON.stringify({
                token_hash: 'test',
                team_id: 'test',
                inviter: 'alice',
                inviter_fingerprint: 'fp',
                invitee: 'bob',
            }),
        });
        expect(res.status).toBe(401);
    });

    it('health endpoint should not require auth', async () => {
        const res = await worker.fetch(`/health`);
        expect(res.status).toBe(200);
        const data = await res.json() as any;
        expect(data.status).toBe('ok');
    });

    it('should allow GET /invites/:hash without auth (public lookup)', async () => {
        const res = await worker.fetch(`/invites/some-hash`);
        // 404 is expected (invite doesn't exist), but NOT 401
        expect(res.status).not.toBe(401);
    });

    it('should reject first-contact registration when the claimed fingerprint does not match the provided public key', async () => {
        const actor = await createIdentity('mismatch');
        const res = await signedFetch(worker, actor, '/teams/test-auth-fingerprint/members/alice', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: 'SHA256:not-the-real-fingerprint',
                public_key: actor.publicKeyB64,
                transport_public_key: transportKey(9),
                transport_fingerprint: transportFingerprint(transportKey(9)),
                role: 'owner',
            }),
        });
        expect(res.status).toBe(400);
    });
});
