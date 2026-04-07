import type { Env } from '../types';
import { logRelayError } from '../middleware/observability';

export type TeamCoordinatorStats = {
    pending_count: number;
    pending_by_recipient: Record<string, number>;
    event_totals: Record<string, number>;
    events_today: Record<string, number>;
    uploads_today: number;
};

export async function recordTeamEvent(env: Env, teamId: string, event: string, delta = 1): Promise<void> {
    try {
        const response = await coordinatorStub(env, teamId).fetch('https://team/event', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ event, delta }),
        });
        if (!response.ok) {
            throw new Error(`team coordinator returned HTTP ${response.status}`);
        }
    } catch (error) {
        logRelayError('team_metrics.record_failed', {
            team_id: teamId,
            event,
            message: error instanceof Error ? error.message : String(error),
        });
    }
}

export async function loadTeamStats(env: Env, teamId: string): Promise<TeamCoordinatorStats> {
    const response = await coordinatorStub(env, teamId).fetch('https://team/stats');
    if (!response.ok) {
        throw new Error(`team coordinator returned HTTP ${response.status}`);
    }
    return response.json<TeamCoordinatorStats>();
}

function coordinatorStub(env: Env, teamId: string): DurableObjectStub {
    const id = env.TEAM_COORDINATOR.idFromName(teamId);
    return env.TEAM_COORDINATOR.get(id);
}
