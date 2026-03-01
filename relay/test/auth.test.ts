/**
 * Auth middleware tests (self-bootstrapping via wrangler unstable_dev).
 *
 * Tests Ed25519 signature verification:
 *   1. Missing auth header is rejected on protected routes
 *   2. Invalid signature format is rejected
 *   3. Health endpoint does not require auth
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { unstable_dev, type UnstableDevWorker } from 'wrangler';

let worker: UnstableDevWorker;

beforeAll(async () => {
    worker = await unstable_dev('src/index.ts', {
        experimental: { disableExperimentalWarning: true },
        vars: {},
    });
});

afterAll(async () => {
    await worker?.stop();
});

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
});
