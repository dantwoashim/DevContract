import { Hono } from 'hono';
import type { Env } from '../types';
import { getTeamTierStatus } from '../middleware/tiers';
import { loadTeamStats } from '../lib/teamCoordinator';
import { loadTeamState } from '../lib/teamState';
import { canReadMetrics } from '../lib/principals';

export const billingRoutes = new Hono<{ Bindings: Env }>();

function billingDisabled(c: any) {
    return c.json({
        error: 'billing_not_enabled',
        message: 'Managed billing is not enabled on this relay deployment.',
    }, 501);
}

billingRoutes.post('/checkout', async (c) => billingDisabled(c));

billingRoutes.post('/webhook', async (c) => billingDisabled(c));

billingRoutes.get('/status/:team', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const { tier, limits } = await getTeamTierStatus(c.env, teamId);

    const stripeSub = await c.env.ENVSYNC_DATA.get(`team:${teamId}:stripe_sub`) || '';
    const updatedAt = await c.env.ENVSYNC_DATA.get(`team:${teamId}:tier_updated_at`) || '';

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
            message: 'Only authorized project principals can view billing status',
        }, 403);
    }

    const stats = await loadTeamStats(c.env, teamId);
    const humanMembers = team.members.filter((member) => member.principal_type !== 'service_principal').length;
    const servicePrincipals = team.members.length - humanMembers;

    return c.json({
        team_id: teamId,
        billing_enabled: false,
        metering_source: 'team_coordinator',
        tier,
        stripe_subscription: stripeSub,
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
