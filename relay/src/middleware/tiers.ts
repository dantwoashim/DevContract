import type { Env } from '../types';

export type TierName = 'free' | 'team' | 'enterprise';

export interface TierLimits {
    maxMembers: number;
    maxBlobsPerDay: number;
    blobTtlHours: number;
    historyDays: number;
}

export interface TierStatus {
    tier: TierName;
    limits: TierLimits;
}

const TIER_CONFIG: Record<TierName, TierLimits> = {
    free: {
        maxMembers: 3,
        maxBlobsPerDay: 10,
        blobTtlHours: 72,
        historyDays: 3,
    },
    team: {
        maxMembers: -1,
        maxBlobsPerDay: -1,
        blobTtlHours: 720,
        historyDays: 30,
    },
    enterprise: {
        maxMembers: -1,
        maxBlobsPerDay: -1,
        blobTtlHours: 8760,
        historyDays: 365,
    },
};

export async function getTeamTier(env: Env, teamId: string): Promise<TierName> {
    const tier = await env.ENVSYNC_DATA.get(`team:${teamId}:tier`);
    if (tier === 'team' || tier === 'enterprise') {
        return tier;
    }
    return 'free';
}

export async function getTeamLimits(env: Env, teamId: string): Promise<TierLimits> {
    const tier = await getTeamTier(env, teamId);
    return TIER_CONFIG[tier];
}

export async function getTeamTierStatus(env: Env, teamId: string): Promise<TierStatus> {
    const tier = await getTeamTier(env, teamId);
    return { tier, limits: TIER_CONFIG[tier] };
}

export async function canAddMember(env: Env, teamId: string, currentCount: number): Promise<boolean> {
    const limits = await getTeamLimits(env, teamId);
    return limits.maxMembers < 0 || currentCount < limits.maxMembers;
}

export async function canUploadBlob(env: Env, teamId: string, currentCount: number): Promise<boolean> {
    const limits = await getTeamLimits(env, teamId);
    return limits.maxBlobsPerDay < 0 || currentCount < limits.maxBlobsPerDay;
}

export async function getBlobTtl(env: Env, teamId: string): Promise<number> {
    const limits = await getTeamLimits(env, teamId);
    return limits.blobTtlHours * 3600;
}

export function limitMessage(limit: string): string {
    return `${limit}. Contact the relay administrator or update the relay deployment entitlements to change this limit.`;
}
