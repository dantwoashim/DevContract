import { Hono } from 'hono';
import type { Env } from '../types';

export const healthRoutes = new Hono<{ Bindings: Env }>();

healthRoutes.get('/', async (c) => {
    return c.json({
        status: 'ok',
        service: 'envsync-relay',
        version: '1.0.0',
        environment: c.env.ENVIRONMENT,
        timestamp: new Date().toISOString(),
    });
});

healthRoutes.get('/live', async (c) => {
    return c.json({
        status: 'live',
        service: 'envsync-relay',
        timestamp: new Date().toISOString(),
    });
});

healthRoutes.get('/ready', async (c) => {
    try {
        await c.env.ENVSYNC_DATA.get('healthcheck:ready');
        const coordinator = c.env.TEAM_COORDINATOR.get(c.env.TEAM_COORDINATOR.idFromName('health'));
        const statsResponse = await coordinator.fetch('https://team/stats');
        const stats = await statsResponse.json<{ pending_count: number }>();
        return c.json({
            status: 'ready',
            service: 'envsync-relay',
            coordinator: 'ok',
            pending_count: stats.pending_count,
            timestamp: new Date().toISOString(),
        });
    } catch (error) {
        return c.json({
            status: 'not_ready',
            service: 'envsync-relay',
            message: error instanceof Error ? error.message : 'KV binding unavailable',
            timestamp: new Date().toISOString(),
        }, 503);
    }
});
