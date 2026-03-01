/**
 * Relay blob CRUD tests (self-bootstrapping via wrangler unstable_dev).
 *
 * Tests encrypted blob lifecycle:
 *   1. Upload blob (PUT /relay/:team/blobs/:id)
 *   2. List pending blobs (GET /relay/:team/pending)
 *   3. Download blob (GET /relay/:team/blobs/:id)
 *   4. Delete blob (DELETE /relay/:team/blobs/:id)
 *   5. Verify deleted blob returns 404
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

const TEAM_ID = 'test-team-relay';
const BLOB_ID = 'blob-' + Date.now();

describe('Relay Blob Operations', () => {
    it('should upload a blob', async () => {
        const res = await worker.fetch(`/relay/${TEAM_ID}/blobs/${BLOB_ID}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                sender_fingerprint: 'fp_alice',
                recipient_fingerprint: 'fp_bob',
                ephemeral_key: 'base64-eph-key',
                filename: '.env',
                data: btoa('ENCRYPTED_DATA_HERE'),
            }),
        });
        expect(res.status).toBeLessThan(300);
    });

    it('should list pending blobs', async () => {
        const res = await worker.fetch(`/relay/${TEAM_ID}/pending`);
        expect(res.status).toBe(200);
        const data = await res.json() as any[];
        expect(data.length).toBeGreaterThanOrEqual(1);
    });

    it('should download a blob', async () => {
        const res = await worker.fetch(`/relay/${TEAM_ID}/blobs/${BLOB_ID}`);
        expect(res.status).toBe(200);
        const data = await res.json() as any;
        expect(data.sender_fingerprint).toBe('fp_alice');
    });

    it('should delete a blob', async () => {
        const res = await worker.fetch(`/relay/${TEAM_ID}/blobs/${BLOB_ID}`, {
            method: 'DELETE',
        });
        expect(res.status).toBeLessThan(300);
    });

    it('should return 404 for deleted blob', async () => {
        const res = await worker.fetch(`/relay/${TEAM_ID}/blobs/${BLOB_ID}`);
        expect(res.status).toBe(404);
    });
});
