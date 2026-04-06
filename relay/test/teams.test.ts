import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { UnstableDevWorker } from 'wrangler';
import { createIdentity, registerMember, signedFetch, startTestWorker, transportFingerprint, transportKey } from './helpers';

let worker: UnstableDevWorker;

beforeAll(async () => {
    worker = await startTestWorker();
});

afterAll(async () => {
    await worker?.stop();
}, 30_000);

describe('Team membership routes', () => {
    const teamId = `test-team-members-${Date.now()}`;
    let owner: Awaited<ReturnType<typeof createIdentity>>;
    let teammate: Awaited<ReturnType<typeof createIdentity>>;

    beforeAll(async () => {
        owner = await createIdentity('team-owner');
        teammate = await createIdentity('team-member');

        expect((await registerMember(worker, owner, teamId, 'alice', transportKey(1), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, owner, `/teams/${teamId}/members/bob`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: teammate.fingerprint,
                public_key: teammate.publicKeyB64,
                transport_public_key: transportKey(2),
                transport_fingerprint: transportFingerprint(transportKey(2)),
                role: 'member',
            }),
        })).status).toBe(200);
    });

    it('removes a member by fingerprint', async () => {
        const res = await signedFetch(worker, owner, `/teams/${teamId}/members/by-fingerprint/${encodeURIComponent(teammate.fingerprint)}`, {
            method: 'DELETE',
        });
        expect(res.status).toBe(200);

        const membersRes = await signedFetch(worker, owner, `/teams/${teamId}/members`);
        expect(membersRes.status).toBe(200);
        const data = await membersRes.json() as { members: Array<{ fingerprint: string }> };
        expect(data.members.some((member) => member.fingerprint === teammate.fingerprint)).toBe(false);
    });
});
