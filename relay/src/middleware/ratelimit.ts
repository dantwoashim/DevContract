import type { Context, Next } from 'hono';
import type { Env } from '../types';

/**
 * Rate limiting middleware.
 * Uses Workers KV to track per-IP and per-team request counts.
 * 
 * Limits:
 *   - Free tier: 100 requests/min per IP
 *   - Pro tier: 500 requests/min per IP 
 *   - Per-team: 200 blobs/day
 */
export async function rateLimitMiddleware(c: Context<{ Bindings: Env }>, next: Next) {
    const ip = c.req.header('cf-connecting-ip') || c.req.header('x-forwarded-for') || 'unknown';
    const key = `ratelimit:${ip}:${Math.floor(Date.now() / 60000)}`; // per-minute window

    try {
        const kv = c.env.ENVSYNC_DATA;
        const current = await kv.get(key);
        const count = current ? parseInt(current, 10) : 0;

        const limit = 100; // Default limit

        if (count >= limit) {
            return c.json({
                error: 'rate_limited',
                message: 'Too many requests. Please wait a moment.',
                retry_after: 60,
            }, 429);
        }

        await kv.put(key, String(count + 1), { expirationTtl: 120 });
    } catch {
        // KV unavailable — fail closed to prevent abuse during outages
        return c.json({
            error: 'service_unavailable',
            message: 'Rate limiting backend unavailable. Try again shortly.',
        }, 503);
    }

    // Set rate limit headers
    c.header('X-RateLimit-Limit', '100');

    await next();
}

/**
 * Team-level rate limiting for blob operations.
 */
export async function teamRateLimitMiddleware(teamID: string, c: Context<{ Bindings: Env }>) {
    const key = `teamlimit:${teamID}:${new Date().toISOString().slice(0, 10)}`;

    try {
        const kv = c.env.ENVSYNC_DATA;
        const current = await kv.get(key);
        const count = current ? parseInt(current, 10) : 0;

        const dailyLimit = 200; // Default for free tier

        if (count >= dailyLimit) {
            return { limited: true, count, limit: dailyLimit };
        }

        await kv.put(key, String(count + 1), { expirationTtl: 86400 });
        return { limited: false, count: count + 1, limit: dailyLimit };
    } catch {
        // KV unavailable — fail closed
        return { limited: true, count: 0, limit: 0 };
    }
}
