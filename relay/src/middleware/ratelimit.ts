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
        const result = await checkRateLimit(c.env, key, limit, 120);

        c.header('X-RateLimit-Limit', String(limit));
        c.header('X-RateLimit-Remaining', String(Math.max(limit-result.count, 0)));

        if (!result.allowed) {
            return c.json({
                error: 'rate_limited',
                message: 'Too many requests. Please wait a moment.',
                retry_after: result.retryAfter,
            }, 429);
        }
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
        const dailyLimit = 200;
        const result = await checkRateLimit(c.env, key, dailyLimit, 86400);
        return { limited: !result.allowed, count: result.count, limit: dailyLimit, degraded: false };
    } catch (error) {
        logRelayError('team_rate_limit.backend_unavailable', {
            team_id: teamID,
            message: error instanceof Error ? error.message : String(error),
        });
        return { limited: false, count: 0, limit: 0, degraded: true };
    }
}

async function checkRateLimit(env: Env, key: string, limit: number, ttlSeconds: number): Promise<{ allowed: boolean; count: number; retryAfter: number }> {
    const id = env.RATE_LIMIT_COORDINATOR.idFromName('global');
    const stub = env.RATE_LIMIT_COORDINATOR.get(id);
    const response = await stub.fetch('https://ratelimit/check', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            key,
            limit,
            ttl_seconds: ttlSeconds,
        }),
    });
    const payload = await response.json<{ allowed: boolean; count: number; retry_after: number }>();
    return {
        allowed: payload.allowed,
        count: payload.count,
        retryAfter: payload.retry_after,
    };
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
