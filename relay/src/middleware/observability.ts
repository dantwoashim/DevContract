import type { Context, Next } from 'hono';
import type { Env } from '../types';

export async function requestIdMiddleware(c: Context<{ Bindings: Env }>, next: Next) {
    const requestId = crypto.randomUUID();
    c.set('requestId' as never, requestId);
    c.header('X-Request-ID', requestId);

    const started = Date.now();
    await next();

    const elapsedMs = Date.now() - started;
    const fingerprint = c.get('fingerprint' as never) as string | undefined;
    console.log(JSON.stringify({
        level: 'info',
        event: 'request.complete',
        request_id: requestId,
        method: c.req.method,
        path: c.req.path,
        status: c.res.status,
        duration_ms: elapsedMs,
        fingerprint: fingerprint || undefined,
    }));
}

export function logRelayEvent(event: string, fields: Record<string, unknown>) {
    console.log(JSON.stringify({
        level: 'info',
        event,
        ...fields,
    }));
}

export function logRelayError(event: string, fields: Record<string, unknown>) {
    console.error(JSON.stringify({
        level: 'error',
        event,
        ...fields,
    }));
}
