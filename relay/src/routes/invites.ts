import { Hono } from 'hono';
import type { Env, Invite, TeamMemberInput } from '../types';
import { computeIdentityFingerprint, computeTransportFingerprint, decodeBase64 } from '../middleware/auth';
import { logRelayEvent } from '../middleware/observability';
import { consumeInviteState, createInviteState, fetchInviteState, joinInviteState, resolveInviteTeam } from '../lib/teamState';

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

    const response = await createInviteState(c.env, body.team_id, invite);
    if (response.ok) {
        logRelayEvent('invite.created', {
            request_id: c.get('requestId' as never),
            team_id: body.team_id,
            actor_fingerprint: actorFingerprint,
            invite_hash: body.token_hash,
            invitee: body.invitee,
        });
    }
    return proxyCoordinatorResponse(response);
});

inviteRoutes.get('/:hash', async (c) => {
    const hash = c.req.param('hash');
    const teamId = await resolveInviteTeam(c.env, hash);
    if (!teamId) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const response = await fetchInviteState(c.env, teamId, hash);
    return proxyCoordinatorResponse(response);
});

inviteRoutes.post('/:hash/consume', consumeInvite);
inviteRoutes.delete('/:hash', consumeInvite);

inviteRoutes.post('/:hash/join', async (c) => {
    const hash = c.req.param('hash');
    const teamId = await resolveInviteTeam(c.env, hash);
    if (!teamId) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const joinerFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput>();
    const username = (body.username || '').trim();

    if (!username || !body.fingerprint || !body.public_key || !body.transport_public_key || !body.transport_fingerprint) {
        return c.json({ error: 'missing_fields', message: 'username, fingerprint, public_key, transport_public_key, and transport_fingerprint are required' }, 400);
    }
    if (joinerFingerprint !== body.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Authenticated fingerprint must match the joining member fingerprint' }, 403);
    }

    try {
        const computedFingerprint = await computeIdentityFingerprint(decodeBase64(body.public_key, 'public_key'));
        const computedTransportFingerprint = await computeTransportFingerprint(decodeBase64(body.transport_public_key, 'transport_public_key'));
        if (computedFingerprint !== body.fingerprint) {
            return c.json({ error: 'invalid_member_keys', message: 'fingerprint does not match public_key' }, 400);
        }
        if (computedTransportFingerprint !== body.transport_fingerprint) {
            return c.json({ error: 'invalid_member_keys', message: 'transport_fingerprint does not match transport_public_key' }, 400);
        }
    } catch (error) {
        return c.json({ error: 'invalid_member_keys', message: error instanceof Error ? error.message : String(error) }, 400);
    }

    const response = await joinInviteState(c.env, teamId, {
        token_hash: hash,
        ...body,
    });
    if (response.ok) {
        await c.env.ENVSYNC_DATA.put(`pubkey:${body.fingerprint}`, body.public_key);
        logRelayEvent('invite.joined', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: joinerFingerprint,
            invite_hash: hash,
        });
    }
    return proxyCoordinatorResponse(response);
});

async function consumeInvite(c: any) {
    const hash = c.req.param('hash');
    const teamId = await resolveInviteTeam(c.env, hash);
    if (!teamId) {
        return c.json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
    }

    const joinerFingerprint = c.get('fingerprint' as never) as string;
    const joinerLabel = (c.req.query('joiner') || '').trim();
    const response = await consumeInviteState(c.env, teamId, {
        token_hash: hash,
        joiner_label: joinerLabel,
        joiner_fingerprint: joinerFingerprint,
    });
    return proxyCoordinatorResponse(response);
}

async function proxyCoordinatorResponse(response: Response) {
    return new Response(await response.text(), {
        status: response.status,
        headers: { 'Content-Type': 'application/json' },
    });
}
