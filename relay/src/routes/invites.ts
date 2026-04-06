import { Hono } from 'hono';
import type { Env, Invite, Team } from '../types';

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

    const team: Team = JSON.parse(teamData);
    const inviterMember = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!inviterMember) {
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
        remaining_attempts: 3,
    };

    await c.env.ENVSYNC_DATA.put(
        `invite:${body.token_hash}`,
        JSON.stringify(invite),
        { expirationTtl: ttlHours * 3600 },
    );

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

    return c.json({
        status: 'consumed',
        team_id: invite.team_id,
        inviter: invite.inviter,
        inviter_fingerprint: invite.inviter_fingerprint,
        joiner_fingerprint: joinerFingerprint,
    });
}
