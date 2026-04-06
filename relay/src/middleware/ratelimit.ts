import type { Context, Next } from 'hono';
import type { Env } from '../types';
import { logRelayError } from './observability';

export async function rateLimitMiddleware(c: Context<{ Bindings: Env }>, next: Next) {
    const ip = c.req.header('cf-connecting-ip') || c.req.header('x-forwarded-for') || 'unknown';
    const limit = requestLimitForPath(c.req.path, c.req.method);
    if (limit <= 0) {
        return next();
    }

    const key = `ratelimit:${ip}:${Math.floor(Date.now() / 60000)}:${limit}`;
    try {
        const kv = c.env.ENVSYNC_DATA;
        const current = await kv.get(key);
        const count = current ? parseInt(current, 10) : 0;

        c.header('X-RateLimit-Limit', String(limit));
        c.header('X-RateLimit-Remaining', String(Math.max(limit-(count+1), 0)));

        if (count >= limit) {
            return c.json({
                error: 'rate_limited',
                message: 'Too many requests. Please wait a moment.',
                retry_after: 60,
            }, 429);
        }

        await kv.put(key, String(count + 1), { expirationTtl: 120 });
    } catch (error) {
        logRelayError('rate_limit.backend_unavailable', {
            path: c.req.path,
            method: c.req.method,
            message: error instanceof Error ? error.message : String(error),
        });
        c.header('X-RateLimit-Bypass', 'backend-unavailable');
    }

    await next();
}

export async function teamRateLimitMiddleware(teamID: string, c: Context<{ Bindings: Env }>) {
    const key = `teamlimit:${teamID}:${new Date().toISOString().slice(0, 10)}`;

    try {
        const kv = c.env.ENVSYNC_DATA;
        const current = await kv.get(key);
        const count = current ? parseInt(current, 10) : 0;

        const dailyLimit = 200;
        if (count >= dailyLimit) {
            return { limited: true, count, limit: dailyLimit, degraded: false };
        }

        await kv.put(key, String(count + 1), { expirationTtl: 86400 });
        return { limited: false, count: count + 1, limit: dailyLimit, degraded: false };
    } catch (error) {
        logRelayError('team_rate_limit.backend_unavailable', {
            team_id: teamID,
            message: error instanceof Error ? error.message : String(error),
        });
        return { limited: false, count: 0, limit: 0, degraded: true };
    }
}

function requestLimitForPath(path: string, method: string): number {
    if (path === '/health') {
        return 0;
    }
    if (path.startsWith('/invites') && method !== 'GET') {
        return 30;
    }
    if (path.startsWith('/relay')) {
        return 120;
    }
    return 100;
}
