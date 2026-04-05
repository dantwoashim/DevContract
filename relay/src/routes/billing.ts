import { Hono } from 'hono';
import type { Env } from '../types';
import { getTeamTierStatus } from '../middleware/tiers';

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
    const { tier, limits } = await getTeamTierStatus(c.env, teamId);

    const stripeSub = await c.env.ENVSYNC_DATA.get(`team:${teamId}:stripe_sub`) || '';
    const updatedAt = await c.env.ENVSYNC_DATA.get(`team:${teamId}:tier_updated_at`) || '';

    const dateKey = new Date().toISOString().split('T')[0];
    const blobCount = parseInt(
        await c.env.ENVSYNC_DATA.get(`ratelimit:blob:${teamId}:${dateKey}`) || '0',
        10,
    );

    const teamData = await c.env.ENVSYNC_DATA.get(`team:${teamId}`);
    let memberCount = 0;
    if (teamData) {
        const team = JSON.parse(teamData);
        memberCount = team.members?.length || 0;
    }

    return c.json({
        team_id: teamId,
        billing_enabled: false,
        tier,
        stripe_subscription: stripeSub,
        updated_at: updatedAt ? parseInt(updatedAt, 10) : null,
        usage: {
            members: memberCount,
            blobs_today: blobCount,
        },
        limits: {
            members: limits.maxMembers,
            blobs_per_day: limits.maxBlobsPerDay,
            history_days: limits.historyDays,
        },
    });
});
