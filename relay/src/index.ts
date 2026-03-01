import { Hono } from 'hono';
import { cors } from 'hono/cors';
import type { Env } from './types';
import { healthRoutes } from './routes/health';
import { inviteRoutes } from './routes/invites';
import { relayRoutes } from './routes/relay';
import { teamRoutes } from './routes/teams';
import { billingRoutes } from './routes/billing';
import { parseAuthHeader, verifySignature, hashBody } from './middleware/auth';
import { rateLimitMiddleware } from './middleware/ratelimit';

const app = new Hono<{ Bindings: Env }>();

// Global middleware
app.use('*', cors({
    origin: '*',
    allowMethods: ['GET', 'POST', 'PUT', 'DELETE'],
    allowHeaders: ['Authorization', 'Content-Type'],
}));

// Global rate limiting
app.use('*', rateLimitMiddleware);

// Auth middleware for all mutating routes (POST, PUT, DELETE)
// Health and GET-only routes are excluded
app.use('/invites/*', async (c, next) => {
    if (c.req.method === 'GET') {
        return next(); // GET invite by hash is public
    }
    return authMiddleware(c, next);
});
app.use('/relay/*', async (c, next) => {
    // All relay operations require authentication
    return authMiddleware(c, next);
});
app.use('/teams/*', async (c, next) => {
    return authMiddleware(c, next);
});

// Auth middleware implementation — verifies ES-SIG header with Ed25519 signature
async function authMiddleware(c: any, next: any) {
    const authHeader = c.req.header('Authorization');
    if (!authHeader) {
        return c.json({ error: 'unauthorized', message: 'Missing Authorization header' }, 401);
    }

    const parsed = parseAuthHeader(authHeader);
    if (!parsed) {
        return c.json({ error: 'unauthorized', message: 'Invalid Authorization header format' }, 401);
    }

    // Check timestamp (5-minute window)
    const now = Math.floor(Date.now() / 1000);
    if (Math.abs(now - parsed.timestamp) > 300) {
        return c.json({ error: 'unauthorized', message: 'Request timestamp too old (5min window)' }, 401);
    }

    // Verify the actual Ed25519 signature
    try {
        // Get the body for hashing
        const bodyClone = await c.req.raw.clone().arrayBuffer();
        const bodyHash = await hashBody(bodyClone);

        // Look up the public key from KV using the fingerprint
        const kv = c.env?.ENVSYNC_KV;
        if (kv) {
            const pubKeyB64 = await kv.get(`pubkey:${parsed.fingerprint}`);
            if (pubKeyB64) {
                // Known key — verify signature against pinned key
                const pubKeyBytes = Uint8Array.from(atob(pubKeyB64), (ch: string) => ch.charCodeAt(0));
                const sigBytes = Uint8Array.from(atob(parsed.signature), (ch: string) => ch.charCodeAt(0));

                const valid = await verifySignature(
                    c.req.method,
                    new URL(c.req.url).pathname,
                    parsed.timestamp,
                    bodyHash,
                    sigBytes,
                    pubKeyBytes,
                );

                if (!valid) {
                    return c.json({ error: 'unauthorized', message: 'Invalid signature' }, 401);
                }
            } else if (parsed.publicKey) {
                // TOFU: First contact — pin the public key, then verify signature
                const pubKeyBytes = Uint8Array.from(atob(parsed.publicKey), (ch: string) => ch.charCodeAt(0));
                const sigBytes = Uint8Array.from(atob(parsed.signature), (ch: string) => ch.charCodeAt(0));

                const valid = await verifySignature(
                    c.req.method,
                    new URL(c.req.url).pathname,
                    parsed.timestamp,
                    bodyHash,
                    sigBytes,
                    pubKeyBytes,
                );

                if (!valid) {
                    return c.json({ error: 'unauthorized', message: 'Invalid signature on first contact' }, 401);
                }

                // Pin the key for future requests
                await kv.put(`pubkey:${parsed.fingerprint}`, parsed.publicKey);
            } else {
                // No pinned key and no public key provided — reject
                return c.json({ error: 'unauthorized', message: 'Unknown fingerprint. Include public key for first-contact registration.' }, 401);
            }
        } else {
            // KV binding unavailable — fail closed, do not silently allow
            return c.json({ error: 'service_unavailable', message: 'Auth backend unavailable' }, 503);
        }
    } catch {
        // Verification infrastructure failure — fail closed
        return c.json({ error: 'service_unavailable', message: 'Signature verification failed' }, 503);
    }

    // Store parsed auth info for route handlers
    c.set('fingerprint', parsed.fingerprint);
    c.set('authTimestamp', parsed.timestamp);

    await next();
}

// Global error handler
app.onError((err, c) => {
    console.error('Unhandled error:', err);
    return c.json({
        error: 'internal_error',
        message: 'An unexpected error occurred',
    }, 500);
});

// Mount routes
app.route('/health', healthRoutes);
app.route('/invites', inviteRoutes);
app.route('/relay', relayRoutes);
app.route('/teams', teamRoutes);
app.route('/billing', billingRoutes);

// 404 handler
app.notFound((c) => {
    return c.json({
        error: 'not_found',
        message: `Route ${c.req.method} ${c.req.path} not found`,
    }, 404);
});

// Export for Cloudflare Workers
export default app;

// Export Durable Object
export { SignalingRoom } from './durable/signaling-room';

