import { Hono } from 'hono';
import type { Env, Team, TeamMember, TeamMemberInput, TeamMetrics } from '../types';
import { computeIdentityFingerprint, computeTransportFingerprint, decodeBase64 } from '../middleware/auth';
import { canAddMember, limitMessage } from '../middleware/tiers';
import { logRelayEvent } from '../middleware/observability';
import { loadTeamStats, recordTeamEvent } from '../lib/teamCoordinator';
import { canAdminMembers, canReadMembers, canReadMetrics, canRotateSelf, isHuman, normalizeMember, normalizePrincipalType, normalizeScopes, normalizeTeam, ownerCount } from '../lib/principals';

export const teamRoutes = new Hono<{ Bindings: Env }>();

teamRoutes.post('/:team/bootstrap', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput & { team_name?: string; bootstrap_nonce?: string; contract_hash?: string }>();
    const username = (body.username || '').trim();
    const founderInput = await validateMemberInput(username, body);
    if ('error' in founderInput) {
        return c.json({ error: 'invalid_member_keys', message: founderInput.error }, 400);
    }
    if (actorFingerprint !== founderInput.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Authenticated fingerprint must match the bootstrap founder' }, 403);
    }
    if (normalizePrincipalType(founderInput.principal_type) !== 'human_member') {
        return c.json({ error: 'invalid_principal', message: 'Only human members can bootstrap a project' }, 400);
    }
    if ((body.bootstrap_nonce || '').trim() === '') {
        return c.json({ error: 'missing_bootstrap_nonce', message: 'bootstrap_nonce is required for project bootstrap' }, 400);
    }

    const existing = await loadTeam(c.env, teamId);
    if (existing) {
        return c.json({ error: 'conflict', message: 'Project already exists on the relay' }, 409);
    }

    const founder: TeamMember = {
        username,
        fingerprint: founderInput.fingerprint,
        public_key: founderInput.public_key,
        transport_public_key: founderInput.transport_public_key,
        transport_fingerprint: founderInput.transport_fingerprint,
        role: 'owner',
        principal_type: 'human_member',
        scopes: [],
        added_at: Math.floor(Date.now() / 1000),
    };

    const team: Team = {
        id: teamId,
        name: (body.team_name || teamId).trim() || teamId,
        members: [founder],
        founded_by: founder.fingerprint,
        founding_nonce_hash: await sha256Hex(body.bootstrap_nonce || ''),
        contract_hash: (body.contract_hash || '').trim() || undefined,
        created_at: Math.floor(Date.now() / 1000),
    };

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await c.env.ENVSYNC_DATA.put(`pubkey:${founder.fingerprint}`, founder.public_key);
    await recordTeamEvent(c.env, teamId, 'team.bootstrapped');
    logRelayEvent('team.bootstrapped', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
    });
    return c.json({ status: 'bootstrapped', member_count: 1, team });
});

teamRoutes.get('/:team/members', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ members: [] });
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!actor || !canReadMembers(actor)) {
        return c.json({ error: 'forbidden', message: 'Only team members can list members' }, 403);
    }

    return c.json({ members: team.members });
});

teamRoutes.get('/:team/metrics', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!actor || !canReadMetrics(actor)) {
        return c.json({ error: 'forbidden', message: 'Only team members can view relay metrics' }, 403);
    }

    const stats = await loadTeamStats(c.env, teamId);
    const payload: TeamMetrics = {
        team_id: teamId,
        member_count: team.members.length,
        pending_count: stats.pending_count,
        pending_by_recipient: stats.pending_by_recipient,
        uploads_today: stats.uploads_today,
        event_totals: stats.event_totals,
        events_today: stats.events_today,
        recorded_at: new Date().toISOString(),
    };
    return c.json(payload);
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

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Project not found. Bootstrap it before adding members.' }, 404);
    }

    {
        const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
        if (!actor) {
            return c.json({ error: 'forbidden', message: 'Only team members can update membership' }, 403);
        }
        if (!canAdminMembers(actor) && actorFingerprint !== memberInput.fingerprint) {
            return c.json({ error: 'forbidden', message: 'Only owners can add or update other members' }, 403);
        }
    }

    const existingIdx = team.members.findIndex((member) => member.fingerprint === memberInput.fingerprint);
    const usernameConflict = team.members.find((member) => member.username === username && member.fingerprint !== memberInput.fingerprint);
    if (usernameConflict) {
        return c.json({ error: 'duplicate_username', message: `Member label ${username} is already used by another fingerprint` }, 409);
    }
    if (memberInput.principal_type === 'service_principal' && memberInput.role === 'owner') {
        return c.json({ error: 'invalid_principal', message: 'Service principals cannot be owners' }, 400);
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
        principal_type: memberInput.principal_type,
        scopes: memberInput.scopes,
        added_at: existing?.added_at || Math.floor(Date.now() / 1000),
    };

    if (existingIdx >= 0) {
        if (team.members[existingIdx].role !== 'owner' && team.members[existingIdx].role !== member.role && !canAdminMembers(team.members.find((candidate) => candidate.fingerprint === actorFingerprint)!)) {
            return c.json({ error: 'forbidden', message: 'Only owners can change member roles' }, 403);
        }
        if (team.members[existingIdx].role === 'owner' && member.role !== 'owner' && ownerCount(team) <= 1) {
            return c.json({ error: 'owner_invariant', message: 'A project must always retain at least one human owner' }, 409);
        }
        if (team.members[existingIdx].role === 'owner') {
            member.role = 'owner';
        }
        team.members[existingIdx] = member;
    } else {
        team.members.push(member);
    }

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await recordTeamEvent(c.env, teamId, 'team.member_upserted');
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
    if (!canAdminMembers(actor) && actor.fingerprint !== target.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
    }
    if (target.role === 'owner' && ownerCount(team) <= 1) {
        return c.json({ error: 'owner_invariant', message: 'A project must always retain at least one human owner' }, 409);
    }

    team.members = team.members.filter((member) => member.username !== username);
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await recordTeamEvent(c.env, teamId, 'team.member_removed');
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
    if (!canAdminMembers(actor) && actor.fingerprint !== target.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
    }
    if (target.role === 'owner' && ownerCount(team) <= 1) {
        return c.json({ error: 'owner_invariant', message: 'A project must always retain at least one human owner' }, 409);
    }

    team.members = team.members.filter((member) => member.fingerprint !== targetFingerprint);
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await recordTeamEvent(c.env, teamId, 'team.member_removed');
    logRelayEvent('team.member_removed', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        member_fingerprint: targetFingerprint,
    });

    return c.json({ status: 'removed', member_count: team.members.length });
});

teamRoutes.post('/:team/transfer-ownership', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<{ fingerprint?: string }>();
    const targetFingerprint = (body.fingerprint || '').trim();
    if (!targetFingerprint) {
        return c.json({ error: 'missing_target', message: 'fingerprint is required' }, 400);
    }

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actorIdx = team.members.findIndex((member) => member.fingerprint === actorFingerprint);
    const targetIdx = team.members.findIndex((member) => member.fingerprint === targetFingerprint);
    if (actorIdx < 0 || targetIdx < 0) {
        return c.json({ error: 'not_found', message: 'Actor or target not found in team' }, 404);
    }

    const actor = team.members[actorIdx];
    const target = team.members[targetIdx];
    if (!isHuman(actor) || actor.role !== 'owner') {
        return c.json({ error: 'forbidden', message: 'Only a human owner can transfer ownership' }, 403);
    }
    if (!isHuman(target)) {
        return c.json({ error: 'invalid_target', message: 'Ownership can only be transferred to a human member' }, 400);
    }

    team.members[actorIdx] = { ...actor, role: 'member' };
    team.members[targetIdx] = { ...target, role: 'owner' };
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await recordTeamEvent(c.env, teamId, 'team.ownership_transferred');
    logRelayEvent('team.ownership_transferred', {
        request_id: c.get('requestId' as never),
        team_id: teamId,
        actor_fingerprint: actorFingerprint,
        target_fingerprint: targetFingerprint,
    });
    return c.json({ status: 'transferred', owner_fingerprint: targetFingerprint });
});

async function loadTeam(env: Env, teamId: string): Promise<Team | null> {
    const data = await env.ENVSYNC_DATA.get(`team:${teamId}`);
    if (!data) {
        return null;
    }
    return normalizeTeam(JSON.parse(data) as Team);
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
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'missing_proof', message: 'proof is required for self-rotation' }, 400);
    }

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actorIdx = team.members.findIndex((member) => member.fingerprint === actorFingerprint);
    if (actorIdx < 0) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'forbidden', message: 'Only team members can rotate identity' }, 403);
    }
    if (!canRotateSelf(team.members[actorIdx])) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'forbidden', message: 'This principal may not rotate itself' }, 403);
    }

    const conflict = team.members.find((member) => member.fingerprint === memberInput.fingerprint && member.fingerprint !== actorFingerprint);
    if (conflict) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'duplicate_fingerprint', message: 'New fingerprint is already registered on this team' }, 409);
    }

    const usernameConflict = team.members.find((member) => member.username === username && member.fingerprint !== actorFingerprint);
    if (usernameConflict) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
        return c.json({ error: 'duplicate_username', message: `Member label ${username} is already used by another fingerprint` }, 409);
    }

    const proofValid = await verifyRotationProof(teamId, actorFingerprint, memberInput, body.proof);
    if (!proofValid) {
        await recordTeamEvent(c.env, teamId, 'team.rotate_failed');
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
        principal_type: existing.principal_type,
        scopes: existing.scopes,
        added_at: existing.added_at,
    };

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    await c.env.ENVSYNC_DATA.put(`pubkey:${memberInput.fingerprint}`, memberInput.public_key);
    await c.env.ENVSYNC_DATA.delete(`pubkey:${actorFingerprint}`);
    await recordTeamEvent(c.env, teamId, 'team.member_rotated');
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

    const normalized = normalizeMember({
        username,
        fingerprint: body.fingerprint,
        public_key: body.public_key,
        transport_public_key: body.transport_public_key,
        transport_fingerprint: body.transport_fingerprint,
        role: body.role,
        principal_type: body.principal_type,
        scopes: normalizeScopes(body.scopes),
    });
    return normalized;
}

async function sha256Hex(value: string): Promise<string> {
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
    return Array.from(new Uint8Array(digest)).map((b) => b.toString(16).padStart(2, '0')).join('');
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
