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

app.use('*', cors({
    origin: '*',
    allowMethods: ['GET', 'POST', 'PUT', 'DELETE'],
    allowHeaders: [
        'Authorization',
        'Content-Type',
        'X-EnvSync-Fingerprint',
        'X-EnvSync-Sender',
        'X-EnvSync-Recipient',
        'X-EnvSync-EphemeralKey',
        'X-EnvSync-Filename',
        'X-EnvSync-Signature',
    ],
}));

app.use('*', rateLimitMiddleware);

app.use('/invites/*', async (c, next) => {
    if (c.req.method === 'GET') {
        return next();
    }
    return authMiddleware(c, next);
});
app.use('/relay/*', authMiddleware);
app.use('/teams/*', authMiddleware);
app.use('/billing/*', authMiddleware);

async function authMiddleware(c: any, next: any) {
    const authHeader = c.req.header('Authorization');
    if (!authHeader) {
        return c.json({ error: 'unauthorized', message: 'Missing Authorization header' }, 401);
    }

    const parsed = parseAuthHeader(authHeader);
    if (!parsed) {
        return c.json({ error: 'unauthorized', message: 'Invalid Authorization header format' }, 401);
    }

    const now = Math.floor(Date.now() / 1000);
    if (Math.abs(now - parsed.timestamp) > 300) {
        return c.json({ error: 'unauthorized', message: 'Request timestamp too old (5min window)' }, 401);
    }

    try {
        const bodyClone = await c.req.raw.clone().arrayBuffer();
        const bodyHash = await hashBody(bodyClone);
        const kv = c.env.ENVSYNC_DATA;
        const pubKeyB64 = await kv.get(`pubkey:${parsed.fingerprint}`);

        if (pubKeyB64) {
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
            await kv.put(`pubkey:${parsed.fingerprint}`, parsed.publicKey);
        } else {
            return c.json({ error: 'unauthorized', message: 'Unknown fingerprint. Include public key for first-contact registration.' }, 401);
        }
    } catch {
        return c.json({ error: 'service_unavailable', message: 'Signature verification failed' }, 503);
    }

    c.set('fingerprint', parsed.fingerprint);
    c.set('authTimestamp', parsed.timestamp);

    await next();
}

app.onError((err, c) => {
    console.error('Unhandled error:', err);
    return c.json({
        error: 'internal_error',
        message: 'An unexpected error occurred',
    }, 500);
});

app.route('/health', healthRoutes);
app.route('/invites', inviteRoutes);
app.route('/relay', relayRoutes);
app.route('/teams', teamRoutes);
app.route('/billing', billingRoutes);

app.notFound((c) => {
    return c.json({
        error: 'not_found',
        message: `Route ${c.req.method} ${c.req.path} not found`,
    }, 404);
});

export default app;
