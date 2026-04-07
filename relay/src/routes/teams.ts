import { Hono } from 'hono';
import type { Env, Team, TeamMember, TeamMemberInput } from '../types';
import { computeIdentityFingerprint, computeTransportFingerprint, decodeBase64 } from '../middleware/auth';
import { canAddMember, limitMessage } from '../middleware/tiers';
import { logRelayEvent } from '../middleware/observability';

export const teamRoutes = new Hono<{ Bindings: Env }>();

teamRoutes.get('/:team/members', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ members: [] });
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!actor) {
        return c.json({ error: 'forbidden', message: 'Only team members can list members' }, 403);
    }

    return c.json({ members: team.members });
});

teamRoutes.put('/:team/members/:user', async (c) => {
    const teamId = c.req.param('team');
    const username = c.req.param('user');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput>();
    const memberInput = await validateMemberInput(username, body);
    if ('error' in memberInput) {
        return c.json({ error: 'invalid_member_keys', message: memberInput.error }, 400);
    }

    let team = await loadTeam(c.env, teamId);
    if (!team) {
        if (actorFingerprint !== memberInput.fingerprint) {
            return c.json({ error: 'forbidden', message: 'Only the authenticated device may bootstrap a team' }, 403);
        }

        team = {
            id: teamId,
            name: teamId,
            members: [],
            created_at: Math.floor(Date.now() / 1000),
        };
    } else {
        const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
        if (!actor) {
            return c.json({ error: 'forbidden', message: 'Only team members can update membership' }, 403);
        }
        if (actor.role !== 'owner' && actorFingerprint !== memberInput.fingerprint) {
            return c.json({ error: 'forbidden', message: 'Only owners can add or update other members' }, 403);
        }
    }

    const existingIdx = team.members.findIndex((member) => member.fingerprint === memberInput.fingerprint);
    const usernameConflict = team.members.find((member) => member.username === username && member.fingerprint !== memberInput.fingerprint);
    if (usernameConflict) {
        return c.json({ error: 'duplicate_username', message: `Member label ${username} is already used by another fingerprint` }, 409);
    }
    if (existingIdx < 0 && !(await canAddMember(c.env, teamId, team.members.length))) {
        return c.json({
            error: 'member_limit',
            message: limitMessage('Team member limit reached'),
        }, 429);
    }

    const existing = existingIdx >= 0 ? team.members[existingIdx] : undefined;
    const member: TeamMember = {
        username,
        fingerprint: memberInput.fingerprint,
        public_key: memberInput.public_key,
        transport_public_key: memberInput.transport_public_key,
        transport_fingerprint: memberInput.transport_fingerprint,
        role: team.members.length === 0 ? 'owner' : (existing?.role || memberInput.role || 'member'),
        added_at: existing?.added_at || Math.floor(Date.now() / 1000),
    };

    if (existingIdx >= 0) {
        if (team.members[existingIdx].role !== 'owner' && team.members[existingIdx].role !== member.role && team.members.find((candidate) => candidate.fingerprint === actorFingerprint)?.role !== 'owner') {
            return c.json({ error: 'forbidden', message: 'Only owners can change member roles' }, 403);
        }
        if (team.members[existingIdx].role === 'owner') {
            member.role = 'owner';
        }
        team.members[existingIdx] = member;
    } else {
        team.members.push(member);
    }

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    logRelayEvent('team.member_upserted', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        member_fingerprint: memberInput.fingerprint,
        status: existingIdx >= 0 ? 'updated' : 'added',
    });
    return c.json({ status: existingIdx >= 0 ? 'updated' : 'added', member_count: team.members.length, member });
});

teamRoutes.delete('/:team/members/:user', async (c) => {
    const teamId = c.req.param('team');
    const username = c.req.param('user');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    const target = team.members.find((member) => member.username === username);
    if (!actor || !target) {
        return c.json({ error: 'not_found', message: `User @${username} not in team` }, 404);
    }
    if (actor.role !== 'owner' && actor.fingerprint !== target.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
    }

    team.members = team.members.filter((member) => member.username !== username);
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    logRelayEvent('team.member_removed', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        member_fingerprint: target.fingerprint,
    });

    return c.json({ status: 'removed', member_count: team.members.length });
});

teamRoutes.delete('/:team/members/by-fingerprint/:fingerprint', async (c) => {
    const teamId = c.req.param('team');
    const targetFingerprint = c.req.param('fingerprint');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    const target = team.members.find((member) => member.fingerprint === targetFingerprint);
    if (!actor || !target) {
        return c.json({ error: 'not_found', message: 'Member fingerprint not found in team' }, 404);
    }
    if (actor.role !== 'owner' && actor.fingerprint !== target.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
    }

    team.members = team.members.filter((member) => member.fingerprint !== targetFingerprint);
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    logRelayEvent('team.member_removed', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        member_fingerprint: targetFingerprint,
    });

    return c.json({ status: 'removed', member_count: team.members.length });
});

async function loadTeam(env: Env, teamId: string): Promise<Team | null> {
    const data = await env.ENVSYNC_DATA.get(`team:${teamId}`);
    if (!data) {
        return null;
    }
    return JSON.parse(data) as Team;
}

teamRoutes.post('/:team/rotate-self', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput & { proof?: string }>();
    const username = (body.username || '').trim();
    const memberInput = await validateMemberInput(username, body);
    if ('error' in memberInput) {
        return c.json({ error: 'invalid_member_keys', message: memberInput.error }, 400);
    }
    if (!body.proof) {
        return c.json({ error: 'missing_proof', message: 'proof is required for self-rotation' }, 400);
    }

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actorIdx = team.members.findIndex((member) => member.fingerprint === actorFingerprint);
    if (actorIdx < 0) {
        return c.json({ error: 'forbidden', message: 'Only team members can rotate identity' }, 403);
    }

    const conflict = team.members.find((member) => member.fingerprint === memberInput.fingerprint && member.fingerprint !== actorFingerprint);
    if (conflict) {
        return c.json({ error: 'duplicate_fingerprint', message: 'New fingerprint is already registered on this team' }, 409);
    }

    const usernameConflict = team.members.find((member) => member.username === username && member.fingerprint !== actorFingerprint);
    if (usernameConflict) {
        return c.json({ error: 'duplicate_username', message: `Member label ${username} is already used by another fingerprint` }, 409);
    }

    const proofValid = await verifyRotationProof(teamId, actorFingerprint, memberInput, body.proof);
    if (!proofValid) {
        return c.json({ error: 'invalid_proof', message: 'Rotation proof did not verify against the replacement identity key' }, 401);
    }

    const existing = team.members[actorIdx];
    team.members[actorIdx] = {
        username,
        fingerprint: memberInput.fingerprint,
        public_key: memberInput.public_key,
        transport_public_key: memberInput.transport_public_key,
        transport_fingerprint: memberInput.transport_fingerprint,
        role: existing.role,
        added_at: existing.added_at,
    };

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await c.env.ENVSYNC_DATA.put(`pubkey:${memberInput.fingerprint}`, memberInput.public_key);
    await c.env.ENVSYNC_DATA.delete(`pubkey:${actorFingerprint}`);
    logRelayEvent('team.member_rotated', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        replacement_fingerprint: memberInput.fingerprint,
    });

    return c.json({ status: 'rotated', fingerprint: memberInput.fingerprint, member_count: team.members.length });
});

async function validateMemberInput(username: string, body: TeamMemberInput): Promise<TeamMemberInput | { error: string }> {
    if (!username || !body.fingerprint || !body.public_key || !body.transport_public_key || !body.transport_fingerprint) {
        return { error: 'username, fingerprint, public_key, transport_public_key, and transport_fingerprint are required' };
    }

    try {
        const computedFingerprint = await computeIdentityFingerprint(decodeBase64(body.public_key, 'public_key'));
        const computedTransportFingerprint = await computeTransportFingerprint(decodeBase64(body.transport_public_key, 'transport_public_key'));
        if (computedFingerprint !== body.fingerprint) {
            return { error: 'fingerprint does not match public_key' };
        }
        if (computedTransportFingerprint !== body.transport_fingerprint) {
            return { error: 'transport_fingerprint does not match transport_public_key' };
        }
    } catch (error) {
        return { error: error instanceof Error ? error.message : String(error) };
    }

    return {
        username,
        fingerprint: body.fingerprint,
        public_key: body.public_key,
        transport_public_key: body.transport_public_key,
        transport_fingerprint: body.transport_fingerprint,
        role: body.role,
    };
}

async function verifyRotationProof(teamId: string, actorFingerprint: string, body: TeamMemberInput, proofB64: string): Promise<boolean> {
    const message = new TextEncoder().encode([
        'rotate-self',
        teamId,
        actorFingerprint,
        body.fingerprint,
        body.transport_fingerprint,
    ].join('\n'));

    const signature = decodeBase64(proofB64, 'proof');
    const publicKey = decodeBase64(body.public_key, 'public_key');
    return crypto.subtle.verify('Ed25519', await crypto.subtle.importKey('raw', publicKey, { name: 'Ed25519' }, false, ['verify']), signature, message);
}
