import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { UnstableDevWorker } from 'wrangler';
import { createIdentity, registerMember, rotationProof, signedFetch, startTestWorker, transportFingerprint, transportKey } from './helpers';

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

    it('updates an existing member by fingerprint when the label changes', async () => {
        expect((await signedFetch(worker, owner, `/teams/${teamId}/members/charlie`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: owner.fingerprint,
                public_key: owner.publicKeyB64,
                transport_public_key: transportKey(1),
                transport_fingerprint: transportFingerprint(transportKey(1)),
                role: 'owner',
            }),
        })).status).toBe(200);

        const membersRes = await signedFetch(worker, owner, `/teams/${teamId}/members`);
        expect(membersRes.status).toBe(200);
        const data = await membersRes.json() as { members: Array<{ fingerprint: string; username: string }> };
        expect(data.members.filter((member) => member.fingerprint === owner.fingerprint)).toHaveLength(1);
        expect(data.members.find((member) => member.fingerprint === owner.fingerprint)?.username).toBe('charlie');
    });

    it('blocks removing the last owner', async () => {
        const res = await signedFetch(worker, owner, `/teams/${teamId}/members/by-fingerprint/${encodeURIComponent(owner.fingerprint)}`, {
            method: 'DELETE',
        });
        expect(res.status).toBe(409);
    });
});

describe('Team identity rotation', () => {
    const teamId = `test-team-rotate-${Date.now()}`;

    it('rotates a non-owner member in place', async () => {
        const owner = await createIdentity('rotate-owner');
        const oldMember = await createIdentity('rotate-old');
        const newMember = await createIdentity('rotate-new');

        expect((await registerMember(worker, owner, teamId, 'owner', transportKey(4), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, owner, `/teams/${teamId}/members/member`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: oldMember.fingerprint,
                public_key: oldMember.publicKeyB64,
                transport_public_key: transportKey(5),
                transport_fingerprint: transportFingerprint(transportKey(5)),
                role: 'member',
            }),
        })).status).toBe(200);

        const replacementTransport = transportKey(6);
        const proof = await rotationProof(newMember, teamId, oldMember.fingerprint, newMember.fingerprint, transportFingerprint(replacementTransport));
        const rotateRes = await signedFetch(worker, oldMember, `/teams/${teamId}/rotate-self`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'member-renamed',
                fingerprint: newMember.fingerprint,
                public_key: newMember.publicKeyB64,
                transport_public_key: replacementTransport,
                transport_fingerprint: transportFingerprint(replacementTransport),
                proof,
            }),
        });

        expect(rotateRes.status).toBe(200);

        const membersRes = await signedFetch(worker, owner, `/teams/${teamId}/members`);
        const data = await membersRes.json() as { members: Array<{ fingerprint: string; username: string }> };
        expect(data.members.find((member) => member.fingerprint === oldMember.fingerprint)).toBeUndefined();
        expect(data.members.find((member) => member.fingerprint === newMember.fingerprint)?.username).toBe('member-renamed');
    });
});

describe('Ownership transfer', () => {
    const teamId = `test-team-transfer-${Date.now()}`;

    it('transfers ownership to another human member', async () => {
        const owner = await createIdentity('transfer-owner');
        const teammate = await createIdentity('transfer-target');

        expect((await registerMember(worker, owner, teamId, 'owner', transportKey(20), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, owner, `/teams/${teamId}/members/teammate`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: teammate.fingerprint,
                public_key: teammate.publicKeyB64,
                transport_public_key: transportKey(21),
                transport_fingerprint: transportFingerprint(transportKey(21)),
                role: 'member',
            }),
        })).status).toBe(200);

        const transferRes = await signedFetch(worker, owner, `/teams/${teamId}/transfer-ownership`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ fingerprint: teammate.fingerprint }),
        });
        expect(transferRes.status).toBe(200);

        const membersRes = await signedFetch(worker, teammate, `/teams/${teamId}/members`);
        const data = await membersRes.json() as { members: Array<{ fingerprint: string; role: string }> };
        expect(data.members.find((member) => member.fingerprint === teammate.fingerprint)?.role).toBe('owner');
        expect(data.members.find((member) => member.fingerprint === owner.fingerprint)?.role).toBe('member');
    });
});

describe('Service principal restrictions', () => {
    const teamId = `test-team-service-${Date.now()}`;

    it('blocks service principals from owner role and admin workflows by default', async () => {
        const owner = await createIdentity('service-owner');
        const service = await createIdentity('service-ci');

        expect((await registerMember(worker, owner, teamId, 'owner', transportKey(30), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, owner, `/teams/${teamId}/members/ci`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                fingerprint: service.fingerprint,
                public_key: service.publicKeyB64,
                transport_public_key: transportKey(31),
                transport_fingerprint: transportFingerprint(transportKey(31)),
                principal_type: 'service_principal',
                scopes: ['relay.pull', 'member.read'],
            }),
        })).status).toBe(200);

        const inviteRes = await signedFetch(worker, service, '/invites', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                token_hash: `service-forbidden-${Date.now()}`,
                team_id: teamId,
                inviter: 'ci',
                inviter_fingerprint: service.fingerprint,
                invitee: 'someone',
            }),
        });
        expect(inviteRes.status).toBe(403);

        const metricsRes = await signedFetch(worker, service, `/teams/${teamId}/metrics`);
        expect(metricsRes.status).toBe(200);
    });
});

describe('Team metrics routes', () => {
    const teamId = `test-team-metrics-${Date.now()}`;

    it('surfaces relay-side counters for operators', async () => {
        const owner = await createIdentity('metrics-owner');
        const joiner = await createIdentity('metrics-joiner');
        const tokenHash = `metrics-token-${Date.now()}`;

        expect((await registerMember(worker, owner, teamId, 'owner', transportKey(12), 'owner')).status).toBe(200);
        expect((await signedFetch(worker, owner, '/invites', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                token_hash: tokenHash,
                team_id: teamId,
                inviter: 'owner',
                inviter_fingerprint: owner.fingerprint,
                invitee: 'joiner',
            }),
        })).status).toBe(201);

        const mismatchTransport = transportKey(13);
        expect((await signedFetch(worker, joiner, `/invites/${tokenHash}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'wrong-name',
                fingerprint: joiner.fingerprint,
                public_key: joiner.publicKeyB64,
                transport_public_key: mismatchTransport,
                transport_fingerprint: transportFingerprint(mismatchTransport),
            }),
        })).status).toBe(409);

        const joinTransport = transportKey(14);
        expect((await signedFetch(worker, joiner, `/invites/${tokenHash}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: 'joiner',
                fingerprint: joiner.fingerprint,
                public_key: joiner.publicKeyB64,
                transport_public_key: joinTransport,
                transport_fingerprint: transportFingerprint(joinTransport),
            }),
        })).status).toBe(200);

        const metricsRes = await signedFetch(worker, owner, `/teams/${teamId}/metrics`);
        expect(metricsRes.status).toBe(200);
        const metrics = await metricsRes.json() as {
            team_id: string;
            member_count: number;
            event_totals: Record<string, number>;
        };

        expect(metrics.team_id).toBe(teamId);
        expect(metrics.member_count).toBe(2);
        expect(metrics.event_totals['invite.created']).toBe(1);
        expect(metrics.event_totals['invite.join_failed']).toBe(1);
        expect(metrics.event_totals['invite.joined']).toBe(1);
    });
});
