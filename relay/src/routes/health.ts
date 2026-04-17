import { Hono } from 'hono';
import type { Env } from '../types';
import { loadTeamStats } from '../lib/teamCoordinator';

export const healthRoutes = new Hono<{ Bindings: Env }>();

healthRoutes.get('/', async (c) => {
    return c.json({
        status: 'ok',
        service: 'devcontract-relay',
        version: '1.0.0',
        environment: c.env.ENVIRONMENT,
        timestamp: new Date().toISOString(),
    });
});

healthRoutes.get('/live', async (c) => {
    return c.json({
        status: 'live',
        service: 'devcontract-relay',
        timestamp: new Date().toISOString(),
    });
});

healthRoutes.get('/ready', async (c) => {
    try {
        await c.env.DEVCONTRACT_DATA.get('healthcheck:ready');
        const stats = await loadTeamStats(c.env, 'health');
        const rateLimitCoordinator = c.env.RATE_LIMIT_COORDINATOR.get(c.env.RATE_LIMIT_COORDINATOR.idFromName('global'));
        const rateLimitResponse = await rateLimitCoordinator.fetch('https://ratelimit/check', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                key: 'healthcheck:ready',
                limit: 1,
                ttl_seconds: 1,
            }),
        });
        if (!rateLimitResponse.ok) {
            throw new Error(`rate limit coordinator returned HTTP ${rateLimitResponse.status}`);
        }
        return c.json({
            status: 'ready',
            service: 'devcontract-relay',
            dependencies: {
                kv: 'ok',
                team_coordinator: 'ok',
                rate_limit_coordinator: 'ok',
            },
            pending_count: stats.pending_count,
            timestamp: new Date().toISOString(),
        });
    } catch (error) {
        return c.json({
            status: 'not_ready',
            service: 'devcontract-relay',
            message: error instanceof Error ? error.message : 'KV binding unavailable',
            timestamp: new Date().toISOString(),
        }, 503);
    }
});
