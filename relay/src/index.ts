import { Hono } from 'hono';
import { cors } from 'hono/cors';
import type { Env } from './types';
import { healthRoutes } from './routes/health';
import { inviteRoutes } from './routes/invites';
import { relayRoutes } from './routes/relay';
import { teamRoutes } from './routes/teams';
import { computeIdentityFingerprint, decodeBase64, parseAuthHeader, verifySignature, hashBody } from './middleware/auth';
import { allowedOrigin } from './middleware/cors';
import { logRelayError, requestIdMiddleware } from './middleware/observability';
import { rateLimitMiddleware } from './middleware/ratelimit';
import { TeamCoordinator } from './durable/teamCoordinator';
import { RateLimitCoordinator } from './durable/rateLimitCoordinator';

const app = new Hono<{ Bindings: Env }>();

app.use('*', requestIdMiddleware);
app.use('*', cors({
    origin: (origin, c) => {
        const configured = allowedOrigin(c.env);
        if (configured === '*') {
            return origin || '*';
        }
        return configured;
    },
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
    exposeHeaders: [
        'X-Request-ID',
        'X-RateLimit-Limit',
        'X-RateLimit-Remaining',
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

    const requestPath = new URL(c.req.url).pathname;

    try {
        const bodyClone = await c.req.raw.clone().arrayBuffer();
        const bodyHash = await hashBody(bodyClone);
        const kv = c.env.ENVSYNC_DATA;
        const pubKeyB64 = await kv.get(`pubkey:${parsed.fingerprint}`);

        if (pubKeyB64) {
            const pubKeyBytes = decodeBase64(pubKeyB64, 'stored public key');
            const sigBytes = decodeBase64(parsed.signature, 'signature');
            const valid = await verifySignature(
                c.req.method,
                requestPath,
                parsed.timestamp,
                bodyHash,
                sigBytes,
                pubKeyBytes,
            );
            if (!valid) {
                return c.json({ error: 'unauthorized', message: 'Invalid signature' }, 401);
            }
        } else if (parsed.publicKey) {
            const pubKeyBytes = decodeBase64(parsed.publicKey, 'public key');
            const computedFingerprint = await computeIdentityFingerprint(pubKeyBytes);
            if (computedFingerprint !== parsed.fingerprint) {
                return c.json({ error: 'unauthorized', message: 'Claimed fingerprint does not match the provided public key' }, 401);
            }

            const sigBytes = decodeBase64(parsed.signature, 'signature');
            const valid = await verifySignature(
                c.req.method,
                requestPath,
                parsed.timestamp,
                bodyHash,
                sigBytes,
                pubKeyBytes,
            );
            if (!valid) {
                return c.json({ error: 'unauthorized', message: 'Invalid signature on first contact' }, 401);
            }

            if (!allowFirstContactEnrollment(c.req.method, requestPath)) {
                return c.json({
                    error: 'registration_required',
                    message: 'Unknown fingerprint. Bootstrap the project or join with a valid invite before using this route.',
                }, 401);
            }

            c.set('firstContact' as never, true);
        } else {
            return c.json({
                error: 'registration_required',
                message: 'Unknown fingerprint. Bootstrap the project or join with a valid invite before using this route.',
            }, 401);
        }
    } catch (error) {
        logRelayError('auth.verification_failed', {
            request_id: c.get('requestId' as never),
            path: requestPath,
            method: c.req.method,
            fingerprint: parsed.fingerprint,
            message: error instanceof Error ? error.message : String(error),
        });
        return c.json({ error: 'service_unavailable', message: 'Signature verification failed' }, 503);
    }

    c.set('fingerprint', parsed.fingerprint);
    c.set('authTimestamp', parsed.timestamp);

    await next();
}

function allowFirstContactEnrollment(method: string, path: string): boolean {
    if (method === 'POST' && /^\/teams\/[^/]+\/bootstrap$/.test(path)) {
        return true;
    }
    if (method === 'POST' && /^\/invites\/[^/]+\/join$/.test(path)) {
        return true;
    }
    return false;
}

app.onError((err, c) => {
    logRelayError('request.unhandled_error', {
        request_id: c.get('requestId' as never),
        path: c.req.path,
        method: c.req.method,
        message: err instanceof Error ? err.message : String(err),
    });
    return c.json({
        error: 'internal_error',
        message: 'An unexpected error occurred',
    }, 500);
});

app.route('/health', healthRoutes);
app.route('/invites', inviteRoutes);
app.route('/relay', relayRoutes);
app.route('/teams', teamRoutes);

app.notFound((c) => {
    return c.json({
        error: 'not_found',
        message: `Route ${c.req.method} ${c.req.path} not found`,
    }, 404);
});

export default app;
export { TeamCoordinator, RateLimitCoordinator };
