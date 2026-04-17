import { Hono } from 'hono';
import type { Env, TeamMemberInput, TeamMetrics } from '../types';
import { computeIdentityFingerprint, computeTransportFingerprint, decodeBase64 } from '../middleware/auth';
import { getTeamLimits, getTeamTierStatus } from '../middleware/tiers';
import { logRelayEvent } from '../middleware/observability';
import { loadTeamStats } from '../lib/teamCoordinator';
import { bootstrapTeamState, listTeamAudit, listTeamInvites, loadTeamState, removeTeamMemberState, revokeInviteState, rotateSelfState, transferOwnershipState, upsertTeamMemberState } from '../lib/teamState';
import { canAdminMembers, canReadMembers, canReadMetrics, normalizeMember, normalizePrincipalType, normalizeScopes } from '../lib/principals';

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

    const response = await bootstrapTeamState(c.env, teamId, {
        founder: founderInput,
        team_name: (body.team_name || teamId).trim() || teamId,
        bootstrap_nonce_hash: await sha256Hex(body.bootstrap_nonce || ''),
        contract_hash: (body.contract_hash || '').trim() || undefined,
    });
    if (response.ok) {
        await c.env.ENVSYNC_DATA.put(`pubkey:${founderInput.fingerprint}`, founderInput.public_key);
        logRelayEvent('team.bootstrapped', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.get('/:team/members', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeamState(c.env, teamId);
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

    const team = await loadTeamState(c.env, teamId);
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

teamRoutes.get('/:team/limits', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const { tier, limits } = await getTeamTierStatus(c.env, teamId);

    const team = await loadTeamState(c.env, teamId);
    if (!team) {
        return c.json({
            error: 'not_found',
            message: 'Team not found',
        }, 404);
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!actor || !canReadMetrics(actor)) {
        return c.json({
            error: 'forbidden',
            message: 'Only authorized project principals can view relay limits',
        }, 403);
    }

    const stats = await loadTeamStats(c.env, teamId);
    const humanMembers = team.members.filter((member) => member.principal_type !== 'service_principal').length;
    const servicePrincipals = team.members.length - humanMembers;
    const updatedAt = await c.env.ENVSYNC_DATA.get(`team:${teamId}:tier_updated_at`) || '';

    return c.json({
        team_id: teamId,
        metering_source: 'team_coordinator',
        tier,
        updated_at: updatedAt ? parseInt(updatedAt, 10) : null,
        usage: {
            member_records: team.members.length,
            human_members: humanMembers,
            service_principals: servicePrincipals,
            blobs_today: stats.uploads_today,
            pending_blobs: stats.pending_count,
        },
        limits: {
            members: limits.maxMembers,
            blobs_per_day: limits.maxBlobsPerDay,
            history_days: limits.historyDays,
        },
    });
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

    const limits = await getTeamLimits(c.env, teamId);
    const response = await upsertTeamMemberState(c.env, teamId, {
        actor_fingerprint: actorFingerprint,
        member: memberInput,
        member_limit: limits.maxMembers,
    });
    if (response.ok) {
        await c.env.ENVSYNC_DATA.put(`pubkey:${memberInput.fingerprint}`, memberInput.public_key);
        logRelayEvent('team.member_upserted', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            member_fingerprint: memberInput.fingerprint,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.delete('/:team/members/:user', async (c) => {
    const teamId = c.req.param('team');
    const username = c.req.param('user');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const response = await removeTeamMemberState(c.env, teamId, {
        actor_fingerprint: actorFingerprint,
        username,
    });
    if (response.ok) {
        logRelayEvent('team.member_removed', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            member_label: username,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.delete('/:team/members/by-fingerprint/:fingerprint', async (c) => {
    const teamId = c.req.param('team');
    const targetFingerprint = c.req.param('fingerprint');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const response = await removeTeamMemberState(c.env, teamId, {
        actor_fingerprint: actorFingerprint,
        fingerprint: targetFingerprint,
    });
    if (response.ok) {
        logRelayEvent('team.member_removed', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            member_fingerprint: targetFingerprint,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.post('/:team/transfer-ownership', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<{ fingerprint?: string }>();
    const targetFingerprint = (body.fingerprint || '').trim();
    if (!targetFingerprint) {
        return c.json({ error: 'missing_target', message: 'fingerprint is required' }, 400);
    }

    const response = await transferOwnershipState(c.env, teamId, {
        actor_fingerprint: actorFingerprint,
        target_fingerprint: targetFingerprint,
    });
    if (response.ok) {
        logRelayEvent('team.ownership_transferred', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            target_fingerprint: targetFingerprint,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.post('/:team/rotate-self', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const body = await c.req.json<TeamMemberInput & { proof?: string }>();
    const username = (body.username || '').trim();
    const memberInput = await validateMemberInput(username, body);
    if ('error' in memberInput) {
        return c.json({ error: 'invalid_member_keys', message: memberInput.error }, 400);
    }

    const response = await rotateSelfState(c.env, teamId, {
        ...memberInput,
        actor_fingerprint: actorFingerprint,
        proof: body.proof,
    });
    if (response.ok) {
        await c.env.ENVSYNC_DATA.put(`pubkey:${memberInput.fingerprint}`, memberInput.public_key);
        if (actorFingerprint !== memberInput.fingerprint) {
            await c.env.ENVSYNC_DATA.delete(`pubkey:${actorFingerprint}`);
        }
        logRelayEvent('team.member_rotated', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            replacement_fingerprint: memberInput.fingerprint,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.get('/:team/invites', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeamState(c.env, teamId);
    const actor = team?.members.find((member) => member.fingerprint === actorFingerprint);
    if (!team || !actor || !canAdminMembers(actor)) {
        return c.json({ error: 'forbidden', message: 'Only project administrators can list invites' }, 403);
    }

    const response = await listTeamInvites(c.env, teamId);
    return proxyCoordinatorResponse(response);
});

teamRoutes.post('/:team/invites/:hash/revoke', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const tokenHash = c.req.param('hash');
    const body = await c.req.json<{ reason?: string }>().catch(() => ({} as { reason?: string }));

    const response = await revokeInviteState(c.env, teamId, {
        actor_fingerprint: actorFingerprint,
        token_hash: tokenHash,
        reason: (body.reason || '').trim() || undefined,
    });
    if (response.ok) {
        logRelayEvent('invite.revoked', {
            request_id: c.get('requestId' as never),
            team_id: teamId,
            actor_fingerprint: actorFingerprint,
            invite_hash: tokenHash,
        });
    }
    return proxyCoordinatorResponse(response);
});

teamRoutes.get('/:team/audit', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const limit = Number(c.req.query('limit') || 20);

    const team = await loadTeamState(c.env, teamId);
    const actor = team?.members.find((member) => member.fingerprint === actorFingerprint);
    if (!team || !actor || !canAdminMembers(actor)) {
        return c.json({ error: 'forbidden', message: 'Only project administrators can view project audit history' }, 403);
    }

    const response = await listTeamAudit(c.env, teamId, limit);
    return proxyCoordinatorResponse(response);
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

    return normalizeMember({
        username,
        fingerprint: body.fingerprint,
        public_key: body.public_key,
        transport_public_key: body.transport_public_key,
        transport_fingerprint: body.transport_fingerprint,
        role: body.role,
        principal_type: body.principal_type,
        scopes: normalizeScopes(body.scopes),
    });
}

async function sha256Hex(value: string): Promise<string> {
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
    return Array.from(new Uint8Array(digest)).map((b) => b.toString(16).padStart(2, '0')).join('');
}

async function proxyCoordinatorResponse(response: Response) {
    return new Response(await response.text(), {
        status: response.status,
        headers: { 'Content-Type': 'application/json' },
    });
}
