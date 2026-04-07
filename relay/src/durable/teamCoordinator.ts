type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

export class TeamCoordinator {
    constructor(private readonly state: DurableObjectState) {}

    async fetch(request: Request): Promise<Response> {
        const url = new URL(request.url);

        if (request.method === 'POST' && url.pathname === '/reserve-upload') {
            const body = await request.json<{ date_key: string; limit: number }>();
            const key = `daily-count:${body.date_key}`;
            const current = Number(await this.state.storage.get<number>(key) || 0);
            if (current >= body.limit) {
                return json({ allowed: false, count: current, limit: body.limit }, 200);
            }
            const next = current + 1;
            await this.state.storage.put(key, next);
            return json({ allowed: true, count: next, limit: body.limit }, 200);
        }

        if (request.method === 'POST' && url.pathname === '/enqueue') {
            const body = await request.json<{ recipient_fingerprint: string; blob_id: string }>();
            const key = pendingKey(body.recipient_fingerprint);
            const pending = (await this.state.storage.get<string[]>(key)) || [];
            if (!pending.includes(body.blob_id)) {
                pending.push(body.blob_id);
                await this.state.storage.put(key, pending);
            }
            return json({ pending }, 200);
        }

        if (request.method === 'POST' && url.pathname === '/event') {
            const body = await request.json<{ event: string; delta?: number }>();
            if (!body.event) {
                return json({ error: 'missing_event' }, 400);
            }

            const delta = typeof body.delta === 'number' && body.delta > 0 ? Math.floor(body.delta) : 1;
            const totalKey = eventTotalKey(body.event);
            const dayKey = eventDayKey(dateKey(), body.event);
            const currentTotal = Number(await this.state.storage.get<number>(totalKey) || 0);
            const currentDay = Number(await this.state.storage.get<number>(dayKey) || 0);
            await this.state.storage.put(totalKey, currentTotal + delta);
            await this.state.storage.put(dayKey, currentDay + delta);
            return json({ status: 'recorded' }, 200);
        }

        if (request.method === 'POST' && url.pathname === '/remove') {
            const body = await request.json<{ recipient_fingerprint: string; blob_id: string }>();
            const key = pendingKey(body.recipient_fingerprint);
            const pending = (await this.state.storage.get<string[]>(key)) || [];
            const filtered = pending.filter((id) => id !== body.blob_id);
            if (filtered.length === 0) {
                await this.state.storage.delete(key);
            } else {
                await this.state.storage.put(key, filtered);
            }
            return json({ pending: filtered }, 200);
        }

        if (request.method === 'GET' && url.pathname === '/pending') {
            const recipientFingerprint = url.searchParams.get('recipient_fingerprint') || '';
            const pending = (await this.state.storage.get<string[]>(pendingKey(recipientFingerprint))) || [];
            return json({ pending }, 200);
        }

        if (request.method === 'GET' && url.pathname === '/stats') {
            const entries = await this.state.storage.list<string[] | number>({ prefix: 'pending:' });
            let pendingCount = 0;
            const pendingByRecipient: Record<string, number> = {};
            for (const value of entries.values()) {
                if (Array.isArray(value)) {
                    pendingCount += value.length;
                }
            }
            for (const [key, value] of entries.entries()) {
                if (Array.isArray(value)) {
                    pendingByRecipient[key.replace('pending:', '')] = value.length;
                }
            }
            const eventTotals = await readCounterPrefix(this.state.storage, 'event_total:');
            const eventsToday = await readCounterPrefix(this.state.storage, `event_day:${dateKey()}:`);
            const uploadsToday = Number(await this.state.storage.get<number>(uploadKey(dateKey())) || 0);
            return json({
                pending_count: pendingCount,
                pending_by_recipient: pendingByRecipient,
                event_totals: eventTotals,
                events_today: eventsToday,
                uploads_today: uploadsToday,
            }, 200);
        }

        return json({ error: 'not_found' }, 404);
    }
}

function pendingKey(recipientFingerprint: string): string {
    return `pending:${recipientFingerprint}`;
}

function uploadKey(date: string): string {
    return `daily-count:${date}`;
}

function eventTotalKey(event: string): string {
    return `event_total:${event}`;
}

function eventDayKey(date: string, event: string): string {
    return `event_day:${date}:${event}`;
}

async function readCounterPrefix(storage: DurableObjectStorage, prefix: string): Promise<Record<string, number>> {
    const counters = await storage.list<number>({ prefix });
    const result: Record<string, number> = {};
    for (const [key, value] of counters.entries()) {
        result[key.slice(prefix.length)] = Number(value || 0);
    }
    return result;
}

function dateKey(): string {
    return new Date().toISOString().slice(0, 10);
}

function json(payload: Record<string, JsonValue>, status: number): Response {
    return new Response(JSON.stringify(payload), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}
