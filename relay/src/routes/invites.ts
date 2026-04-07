import { Hono } from 'hono';
import type { Env, Invite, Team, TeamMember, TeamMemberInput } from '../types';
import { computeIdentityFingerprint, computeTransportFingerprint, decodeBase64 } from '../middleware/auth';
import { logRelayEvent } from '../middleware/observability';
import { recordTeamEvent } from '../lib/teamCoordinator';
import { canCreateInvite, normalizeMember, normalizeTeam } from '../lib/principals';

export const inviteRoutes = new Hono<{ Bindings: Env }>();

inviteRoutes.post('/', async (c) => {
    const body = await c.req.json<{
        token_hash: string;
        team_id: string;
        inviter: string;
        inviter_fingerprint: string;
        invitee: string;
    }>();

    if (!body.token_hash || !body.team_id || !body.inviter_fingerprint || !body.invitee) {
        return c.json({ error: 'missing_fields', message: 'token_hash, team_id, inviter_fingerprint, and invitee are required' }, 400);
    }

    const actorFingerprint = c.get('fingerprint' as never) as string;
    if (actorFingerprint !== body.inviter_fingerprint) {
        return c.json({ error: 'forbidden', message: 'Authenticated fingerprint must match inviter_fingerprint' }, 403);
    }

    const teamData = await c.env.ENVSYNC_DATA.get(`team:${body.team_id}`);
    if (!teamData) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const team: Team = normalizeTeam(JSON.parse(teamData));
    const inviterMember = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!inviterMember || !canCreateInvite(inviterMember)) {
        return c.json({ error: 'forbidden', message: 'Inviter is not a team member' }, 403);
    }

    const existing = await c.env.ENVSYNC_DATA.get(`invite:${body.token_hash}`);
    if (existing) {
        return c.json({ error: 'duplicate', message: 'An invite with this token already exists' }, 409);
    }

    const ttlHours = parseInt(c.env.INVITE_TTL_HOURS || '24', 10);
    const now = Math.floor(Date.now() / 1000);

    const invite: Invite = {
        token_hash: body.token_hash,
        team_id: body.team_id,
        inviter: body.inviter,
        inviter_fingerprint: body.inviter_fingerprint,
        invitee: body.invitee,
        created_at: now,
        expires_at: now + (ttlHours * 3600),
        consumed: false,
    };

    await c.env.ENVSYNC_DATA.put(
        `invite:${body.token_hash}`,
        JSON.stringify(invite),
        { expirationTtl: ttlHours * 3600 },
    );

    await recordTeamEvent(c.env, body.team_id, 'invite.created');
    logRelayEvent('invite.created', {
        request_id: c.get('requestId' as never),
        team_id: body.team_id,
        actor_fingerprint: actorFingerprint,
        invite_hash: body.token_hash,
        invitee: body.invitee,
    });

    return c.json({ status: 'created', expires_at: invite.expires_at }, 201);
});

inviteRoutes.get('/:hash', async (c) => {
    const hash = c.req.param('hash');
    const data = await c.env.ENVSYNC_DATA.get(`invite:${hash}`);

    if (!data) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const invite: Invite = JSON.parse(data);
    if (invite.consumed) {
        return c.json({ error: 'consumed', message: 'This invite has already been used' }, 410);
    }

    return c.json({
        team_id: invite.team_id,
        inviter: invite.inviter,
        inviter_fingerprint: invite.inviter_fingerprint,
        expires_at: invite.expires_at,
    });
});

inviteRoutes.post('/:hash/consume', consumeInvite);
inviteRoutes.delete('/:hash', consumeInvite);
inviteRoutes.post('/:hash/join', async (c) => {
    const hash = c.req.param('hash');
    const joinerFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput>();
    const username = (body.username || '').trim();

    if (!username || !body.fingerprint || !body.public_key || !body.transport_public_key || !body.transport_fingerprint) {
        return c.json({ error: 'missing_fields', message: 'username, fingerprint, public_key, transport_public_key, and transport_fingerprint are required' }, 400);
    }
    if (joinerFingerprint !== body.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Authenticated fingerprint must match the joining member fingerprint' }, 403);
    }

    let computedFingerprint: string;
    let computedTransportFingerprint: string;
    try {
        computedFingerprint = await computeIdentityFingerprint(decodeBase64(body.public_key, 'public_key'));
        computedTransportFingerprint = await computeTransportFingerprint(decodeBase64(body.transport_public_key, 'transport_public_key'));
    } catch (error) {
        return c.json({ error: 'invalid_member_keys', message: error instanceof Error ? error.message : String(error) }, 400);
    }

    if (computedFingerprint !== body.fingerprint) {
        return c.json({ error: 'invalid_member_keys', message: 'fingerprint does not match public_key' }, 400);
    }
    if (computedTransportFingerprint !== body.transport_fingerprint) {
        return c.json({ error: 'invalid_member_keys', message: 'transport_fingerprint does not match transport_public_key' }, 400);
    }

    const data = await c.env.ENVSYNC_DATA.get(`invite:${hash}`);
    if (!data) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const invite: Invite = JSON.parse(data);
    if (invite.consumed) {
        return c.json({ error: 'consumed', message: 'This invite has already been used' }, 410);
    }

    if (invite.invitee && invite.invitee !== username) {
        await recordTeamEvent(c.env, invite.team_id, 'invite.join_failed');
        return c.json({
            error: 'invite_mismatch',
            message: `Invite was issued for ${invite.invitee}, not ${username}`,
        }, 409);
    }

    const teamData = await c.env.ENVSYNC_DATA.get(`team:${invite.team_id}`);
    if (!teamData) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const team: Team = normalizeTeam(JSON.parse(teamData));
    const usernameConflict = team.members.find((member) => member.username === username && member.fingerprint !== body.fingerprint);
    if (usernameConflict) {
        await recordTeamEvent(c.env, invite.team_id, 'invite.join_failed');
        return c.json({ error: 'duplicate_username', message: `Member label ${username} is already used by another fingerprint` }, 409);
    }

    const existingIdx = team.members.findIndex((member) => member.fingerprint === body.fingerprint);
    const existing = existingIdx >= 0 ? team.members[existingIdx] : undefined;
    const member: TeamMember = normalizeMember({
        username,
        fingerprint: body.fingerprint,
        public_key: body.public_key,
        transport_public_key: body.transport_public_key,
        transport_fingerprint: body.transport_fingerprint,
        role: existing?.role || 'member',
        added_at: existing?.added_at || Math.floor(Date.now() / 1000),
    });

    if (existingIdx >= 0) {
        team.members[existingIdx] = member;
    } else {
        team.members.push(member);
    }

    invite.consumed = true;
    await c.env.ENVSYNC_DATA.put(`team:${invite.team_id}`, JSON.stringify(team));
    await c.env.ENVSYNC_DATA.put(`pubkey:${body.fingerprint}`, body.public_key);
    await c.env.ENVSYNC_DATA.put(
        `invite:${hash}`,
        JSON.stringify(invite),
        { expirationTtl: 60 },
    );

    await recordTeamEvent(c.env, invite.team_id, 'invite.joined');
    logRelayEvent('invite.joined', {
        request_id: c.get('requestId' as never),
        team_id: invite.team_id,
        actor_fingerprint: joinerFingerprint,
        invite_hash: hash,
    });

    return c.json({
        status: 'joined',
        team_id: invite.team_id,
        inviter: invite.inviter,
        inviter_fingerprint: invite.inviter_fingerprint,
        joiner_fingerprint: joinerFingerprint,
        members: team.members.filter((member) => member.principal_type !== 'service_principal'),
    });
});

async function consumeInvite(c: any) {
    const hash = c.req.param('hash');
    const joinerFingerprint = c.get('fingerprint' as never) as string;
    const joinerLabel = (c.req.query('joiner') || '').trim();

    const data = await c.env.ENVSYNC_DATA.get(`invite:${hash}`);
    if (!data) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const invite: Invite = JSON.parse(data);
    if (invite.consumed) {
        return c.json({ error: 'consumed', message: 'This invite has already been used' }, 410);
    }

    if (invite.invitee && joinerLabel && invite.invitee !== joinerLabel) {
        return c.json({
            error: 'invite_mismatch',
            message: `Invite was issued for ${invite.invitee}, not ${joinerLabel}`,
        }, 409);
    }

    invite.consumed = true;
    await c.env.ENVSYNC_DATA.put(
        `invite:${hash}`,
        JSON.stringify(invite),
        { expirationTtl: 60 },
    );

    await recordTeamEvent(c.env, invite.team_id, 'invite.consumed');
    return c.json({
        status: 'consumed',
        team_id: invite.team_id,
        inviter: invite.inviter,
        inviter_fingerprint: invite.inviter_fingerprint,
        joiner_fingerprint: joinerFingerprint,
    });
}
