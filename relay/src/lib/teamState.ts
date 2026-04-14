import type { Env, Team, TeamAuditEvent } from '../types';

export type AuditEventInput = Omit<TeamAuditEvent, 'id' | 'team_id' | 'created_at'>;

export async function loadTeamState(env: Env, teamId: string): Promise<Team | null> {
    const response = await controlPlane(env, teamId).fetch('https://team/team', {
        headers: teamHeaders(teamId),
    });
    if (response.status === 404) {
        return null;
    }
    if (!response.ok) {
        throw new Error(`team coordinator returned HTTP ${response.status}`);
    }
    const payload = await response.json<{ team: Team }>();
    return payload.team || null;
}

export async function bootstrapTeamState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/team/bootstrap', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function upsertTeamMemberState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/member/upsert', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function removeTeamMemberState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/member/remove', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function transferOwnershipState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/member/transfer-ownership', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function rotateSelfState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/member/rotate-self', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function createInviteState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/invite/create', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function fetchInviteState(env: Env, teamId: string, tokenHash: string): Promise<Response> {
    return controlPlane(env, teamId).fetch(`https://team/invite?token_hash=${encodeURIComponent(tokenHash)}`, {
        headers: teamHeaders(teamId),
    });
}

export async function consumeInviteState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/invite/consume', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function joinInviteState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/invite/join', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function listTeamInvites(env: Env, teamId: string): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/invites', {
        headers: teamHeaders(teamId),
    });
}

export async function revokeInviteState(env: Env, teamId: string, payload: unknown): Promise<Response> {
    return controlPlane(env, teamId).fetch('https://team/invite/revoke', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
    });
}

export async function listTeamAudit(env: Env, teamId: string, limit: number): Promise<Response> {
    return controlPlane(env, teamId).fetch(`https://team/audit?limit=${Math.max(1, Math.min(limit, 100))}`, {
        headers: teamHeaders(teamId),
    });
}

export async function appendTeamAuditEvent(env: Env, teamId: string, event: AuditEventInput): Promise<void> {
    const response = await controlPlane(env, teamId).fetch('https://team/audit', {
        method: 'POST',
        headers: {
            ...teamHeaders(teamId),
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(event),
    });
    if (!response.ok) {
        throw new Error(`team coordinator returned HTTP ${response.status}`);
    }
}

export async function resolveInviteTeam(env: Env, tokenHash: string): Promise<string | null> {
    const cacheKey = inviteRefKey(tokenHash);
    const cached = (await env.ENVSYNC_DATA.get(cacheKey)) || '';
    if (cached) {
        return cached;
    }

    const legacy = await env.ENVSYNC_DATA.get(`invite:${tokenHash}`);
    if (!legacy) {
        return null;
    }

    const parsed = JSON.parse(legacy) as { team_id?: string; expires_at?: number };
    const teamId = (parsed.team_id || '').trim();
    if (!teamId) {
        return null;
    }

    const ttlSeconds = ttlFromUnix(parsed.expires_at || 0, 7 * 24 * 3600);
    await env.ENVSYNC_DATA.put(cacheKey, teamId, { expirationTtl: ttlSeconds });
    return teamId;
}

function controlPlane(env: Env, teamId: string): DurableObjectStub {
    const id = env.TEAM_COORDINATOR.idFromName(teamId);
    return env.TEAM_COORDINATOR.get(id);
}

function teamHeaders(teamId: string): HeadersInit {
    return { 'X-EnvSync-Team-ID': teamId };
}

function inviteRefKey(tokenHash: string): string {
    return `invite-ref:${tokenHash}`;
}

function ttlFromUnix(expiresAt: number, fallbackSeconds: number): number {
    if (expiresAt <= 0) {
        return fallbackSeconds;
    }
    const ttl = expiresAt - Math.floor(Date.now() / 1000);
    return ttl > 60 ? ttl : fallbackSeconds;
}
