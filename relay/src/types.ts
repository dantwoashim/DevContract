// EnvSync Relay — Shared Types

export interface Env {
    ENVSYNC_DATA: KVNamespace;
    TEAM_COORDINATOR: DurableObjectNamespace;
    RATE_LIMIT_COORDINATOR: DurableObjectNamespace;
    ENVIRONMENT: string;
    MAX_BLOB_SIZE: string;
    INVITE_TTL_HOURS: string;
    BLOB_TTL_HOURS: string;
    BILLING_ENABLED?: string;
    CORS_ALLOW_ORIGIN?: string;
}

export interface Invite {
    token_hash: string;
    team_id: string;
    inviter: string;
    inviter_fingerprint: string;
    invitee: string;
    created_at: number;
    expires_at: number;
    consumed: boolean;
}

export interface BlobMetadata {
    blob_id: string;
    team_id: string;
    sender_fingerprint: string;
    recipient_fingerprint: string;
    ephemeral_public_key: string;
    sender_signature: string;
    size: number;
    uploaded_at: number;
    expires_at: number;
    filename: string;
}

export interface TeamMember {
    username: string;
    fingerprint: string;
    public_key: string;
    transport_public_key: string;
    transport_fingerprint: string;
    role: 'owner' | 'member';
    added_at: number;
}

export interface Team {
    id: string;
    name: string;
    members: TeamMember[];
    created_at: number;
}

export interface AuthInfo {
    fingerprint: string;
    timestamp: number;
    verified: boolean;
}

export interface TeamMemberInput {
    username: string;
    fingerprint: string;
    public_key: string;
    transport_public_key: string;
    transport_fingerprint: string;
    role?: 'owner' | 'member';
}

export interface TeamMetrics {
    team_id: string;
    member_count: number;
    pending_count: number;
    pending_by_recipient: Record<string, number>;
    uploads_today: number;
    event_totals: Record<string, number>;
    events_today: Record<string, number>;
    recorded_at: string;
}
