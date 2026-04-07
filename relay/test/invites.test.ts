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

    it('should join the team in one flow', async () => {
        const joinTransport = transportKey(7);
        const res = await signedFetch(worker, joiner, `/invites/${tokenHash}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'bob',
                fingerprint: joiner.fingerprint,
                public_key: joiner.publicKeyB64,
                transport_public_key: joinTransport,
                transport_fingerprint: transportFingerprint(joinTransport),
            }),
        });
        expect(res.status).toBe(200);
        const data = await res.json() as any;
        expect(data.team_id).toBe(teamId);
        expect(data.inviter_fingerprint).toBe(owner.fingerprint);
        expect(data.members.some((member: any) => member.fingerprint === joiner.fingerprint)).toBe(true);
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

describe('Invite joiner validation', () => {
    const tokenHash = `test-token-mismatch-${Date.now()}`;
    const teamId = `test-team-mismatch-${Date.now()}`;

    let owner: Awaited<ReturnType<typeof createIdentity>>;
    let joiner: Awaited<ReturnType<typeof createIdentity>>;

    beforeAll(async () => {
        owner = await createIdentity('invite-owner-mismatch');
        joiner = await createIdentity('invite-joiner-mismatch');

        const bootstrap = await registerMember(worker, owner, teamId, 'alice', transportKey(3), 'owner');
        expect(bootstrap.status).toBe(200);

        const create = await signedFetch(worker, owner, '/invites', {
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
        expect(create.status).toBe(201);
    });

    it('rejects consume when joiner label does not match invite target', async () => {
        const joinTransport = transportKey(9);
        const res = await signedFetch(worker, joiner, `/invites/${tokenHash}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'mallory',
                fingerprint: joiner.fingerprint,
                public_key: joiner.publicKeyB64,
                transport_public_key: joinTransport,
                transport_fingerprint: transportFingerprint(joinTransport),
            }),
        });
        expect(res.status).toBe(409);
    });

    it('keeps the invite usable after a mismatch', async () => {
        const joinTransport = transportKey(10);
        const res = await signedFetch(worker, joiner, `/invites/${tokenHash}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'bob',
                fingerprint: joiner.fingerprint,
                public_key: joiner.publicKeyB64,
                transport_public_key: joinTransport,
                transport_fingerprint: transportFingerprint(joinTransport),
            }),
        });
        expect(res.status).toBe(200);
    });
});

describe('Invite administration', () => {
    const tokenHash = `test-token-admin-${Date.now()}`;
    const teamId = `test-team-admin-${Date.now()}`;

    let owner: Awaited<ReturnType<typeof createIdentity>>;

    beforeAll(async () => {
        owner = await createIdentity('invite-admin-owner');
        const bootstrap = await registerMember(worker, owner, teamId, 'owner', transportKey(11), 'owner');
        expect(bootstrap.status).toBe(200);
        const create = await signedFetch(worker, owner, '/invites', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                token_hash: tokenHash,
                team_id: teamId,
                inviter: 'owner',
                inviter_fingerprint: owner.fingerprint,
                invitee: 'pending-user',
            }),
        });
        expect(create.status).toBe(201);
    });

    it('lists relay invites for administrators', async () => {
        const res = await signedFetch(worker, owner, `/teams/${teamId}/invites`);
        expect(res.status).toBe(200);
        const data = await res.json() as { invites: Array<{ token_hash: string; status: string }> };
        expect(data.invites.some((invite) => invite.token_hash === tokenHash && invite.status === 'pending')).toBe(true);
    });

    it('revokes a pending invite and exposes the audit trail', async () => {
        const revoke = await signedFetch(worker, owner, `/teams/${teamId}/invites/${tokenHash}/revoke`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ reason: 'manual test revoke' }),
        });
        expect(revoke.status).toBe(200);

        const invite = await worker.fetch(`/invites/${tokenHash}`);
        expect(invite.status).toBe(410);
        const invitePayload = await invite.json() as { error: string };
        expect(invitePayload.error).toBe('revoked');

        const audit = await signedFetch(worker, owner, `/teams/${teamId}/audit?limit=10`);
        expect(audit.status).toBe(200);
        const auditPayload = await audit.json() as { events: Array<{ action: string; invite_hash?: string; result: string }> };
        expect(auditPayload.events.some((event) => event.action === 'invite.created' && event.invite_hash === tokenHash && event.result === 'succeeded')).toBe(true);
        expect(auditPayload.events.some((event) => event.action === 'invite.revoked' && event.invite_hash === tokenHash && event.result === 'succeeded')).toBe(true);
    });
});
