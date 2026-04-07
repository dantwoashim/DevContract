type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

export class RateLimitCoordinator {
    constructor(private readonly state: DurableObjectState) {}

    async fetch(request: Request): Promise<Response> {
        const url = new URL(request.url);
        if (request.method !== 'POST' || url.pathname !== '/check') {
            return json({ error: 'not_found' }, 404);
        }

        const body = await request.json<{ key: string; limit: number; ttl_seconds: number }>();
        const now = Date.now();
        const current = await this.state.storage.get<{ count: number; expires_at: number }>(body.key);

        let count = 0;
        let expiresAt = now + (body.ttl_seconds * 1000);
        if (current && current.expires_at > now) {
            count = current.count;
            expiresAt = current.expires_at;
        }

        if (count >= body.limit) {
            return json({ allowed: false, count, limit: body.limit, retry_after: Math.max(Math.ceil((expiresAt - now) / 1000), 1) }, 200);
        }

        const next = {
            count: count + 1,
            expires_at: expiresAt,
        };
        await this.state.storage.put(body.key, next);
        return json({ allowed: true, count: next.count, limit: body.limit, retry_after: Math.max(Math.ceil((expiresAt - now) / 1000), 1) }, 200);
    }
}

function json(payload: Record<string, JsonValue>, status: number): Response {
    return new Response(JSON.stringify(payload), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}
