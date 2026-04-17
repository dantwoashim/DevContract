// DevContract Relay — Shared Types

export interface Env {
    DEVCONTRACT_DATA: KVNamespace;
    TEAM_COORDINATOR: DurableObjectNamespace;
    RATE_LIMIT_COORDINATOR: DurableObjectNamespace;
    ENVIRONMENT: string;
    MAX_BLOB_SIZE: string;
    INVITE_TTL_HOURS: string;
    BLOB_TTL_HOURS: string;
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
    consumed_at?: number;
    consumed_by_fingerprint?: string;
    revoked_at?: number;
    revoked_by_fingerprint?: string;
    revoke_reason?: string;
    status?: 'pending' | 'consumed' | 'revoked' | 'expired';
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
    status?: 'pending' | 'handled' | 'rejected_client' | 'quarantined_server' | 'expired';
    failure_reason?: string;
    rejected_at?: number;
}

export type PrincipalType = 'human_member' | 'service_principal';
export type PrincipalScope =
    | 'member.read'
    | 'relay.pull'
    | 'relay.push'
    | 'invite.create'
    | 'member.rotate.self'
    | 'admin.members'
    | 'metrics.read';

export interface TeamMember {
    username: string;
    fingerprint: string;
    public_key: string;
    transport_public_key: string;
    transport_fingerprint: string;
    role: 'owner' | 'member';
    principal_type?: PrincipalType;
    scopes?: PrincipalScope[];
    added_at: number;
}

export interface Team {
    id: string;
    name: string;
    members: TeamMember[];
    founded_by?: string;
    founding_nonce_hash?: string;
    contract_hash?: string;
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
    principal_type?: PrincipalType;
    scopes?: PrincipalScope[];
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

export interface TeamAuditEvent {
    id: string;
    team_id: string;
    action: string;
    actor_fingerprint?: string;
    actor_principal_type?: PrincipalType;
    actor_scopes?: PrincipalScope[];
    target_fingerprint?: string;
    invite_hash?: string;
    blob_id?: string;
    result: 'succeeded' | 'failed';
    details?: string;
    created_at: number;
}
