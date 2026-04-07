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
