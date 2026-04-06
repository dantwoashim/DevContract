import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import type { UnstableDevWorker } from 'wrangler';
import { createIdentity, registerMember, signedFetch, startTestWorker, transportKey } from './helpers';

let worker: UnstableDevWorker;

beforeAll(async () => {
    worker = await startTestWorker();
});

afterAll(async () => {
    await worker?.stop();
}, 30_000);

describe('Invite Flow', () => {
    const tokenHash = `test-token-hash-${Date.now()}`;
    const teamId = `test-team-id-${Date.now()}`;

    let owner: Awaited<ReturnType<typeof createIdentity>>;
    let joiner: Awaited<ReturnType<typeof createIdentity>>;

    beforeAll(async () => {
        owner = await createIdentity('invite-owner');
        joiner = await createIdentity('invite-joiner');

        const bootstrap = await registerMember(worker, owner, teamId, 'alice', transportKey(1), 'owner');
        expect(bootstrap.status).toBe(200);
    });

    it('should create an invite', async () => {
        const res = await signedFetch(worker, owner, '/invites', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                token_hash: tokenHash,
                team_id: teamId,
                inviter: 'alice',
                inviter_fingerprint: owner.fingerprint,
                invitee: 'bob',
            }),
        });
        expect(res.status).toBe(201);
    });

    it('should retrieve the invite', async () => {
        const res = await worker.fetch(`/invites/${tokenHash}`);
        expect(res.status).toBe(200);
        const data = await res.json() as any;
        expect(data.team_id).toBe(teamId);
        expect(data.inviter).toBe('alice');
    });

    it('should consume the invite', async () => {
        const res = await signedFetch(worker, joiner, `/invites/${tokenHash}`, {
            method: 'DELETE',
        });
        expect(res.status).toBe(200);
        const data = await res.json() as any;
        expect(data.team_id).toBe(teamId);
        expect(data.inviter_fingerprint).toBe(owner.fingerprint);
    });

    it('should reject already-consumed invite', async () => {
        const res = await signedFetch(worker, joiner, `/invites/${tokenHash}`, {
            method: 'DELETE',
        });
        expect(res.status).toBe(410);
    });

    it('should return 404 for unknown invite', async () => {
        const res = await worker.fetch('/invites/nonexistent-hash');
        expect(res.status).toBe(404);
    });
});
