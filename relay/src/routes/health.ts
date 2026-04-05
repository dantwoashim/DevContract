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
        return c.json({
            status: 'ready',
            service: 'envsync-relay',
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
