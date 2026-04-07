import type { PrincipalScope, PrincipalType, Team, TeamMember, TeamMemberInput } from '../types';

const VALID_SCOPES: ReadonlySet<PrincipalScope> = new Set([
    'member.read',
    'relay.pull',
    'relay.push',
    'invite.create',
    'member.rotate.self',
    'admin.members',
    'metrics.read',
]);

export function normalizePrincipalType(value?: string): PrincipalType {
    return value === 'service_principal' ? 'service_principal' : 'human_member';
}

export function normalizeScopes(scopes?: string[]): PrincipalScope[] {
    const unique = new Set<PrincipalScope>();
    for (const scope of scopes || []) {
        if (VALID_SCOPES.has(scope as PrincipalScope)) {
            unique.add(scope as PrincipalScope);
        }
    }
    return [...unique].sort();
}

export function normalizeMember<T extends TeamMember | TeamMemberInput>(member: T): T {
    const principalType = normalizePrincipalType(member.principal_type);
    const scopes = normalizeScopes(member.scopes);
    const role = principalType === 'service_principal' ? 'member' : (member.role === 'owner' ? 'owner' : 'member');
    return {
        ...member,
        principal_type: principalType,
        role,
        scopes,
    };
}

export function normalizeTeam(team: Team): Team {
    return {
        ...team,
        members: team.members.map((member) => normalizeMember(member)),
    };
}

export function isHuman(member: TeamMember): boolean {
    return normalizePrincipalType(member.principal_type) === 'human_member';
}

export function isOwner(member: TeamMember): boolean {
    return member.role === 'owner';
}

export function ownerCount(team: Team): number {
    return team.members.filter((member) => member.role === 'owner' && isHuman(member)).length;
}

export function hasScope(member: TeamMember, scope: PrincipalScope): boolean {
    return normalizeScopes(member.scopes).includes(scope);
}

export function canReadMembers(member: TeamMember): boolean {
    return isHuman(member) || hasScope(member, 'member.read') || hasScope(member, 'admin.members');
}

export function canReadMetrics(member: TeamMember): boolean {
    return isHuman(member) || hasScope(member, 'metrics.read') || hasScope(member, 'member.read') || hasScope(member, 'admin.members');
}

export function canCreateInvite(member: TeamMember): boolean {
    return (isHuman(member) && isOwner(member)) || hasScope(member, 'invite.create') || hasScope(member, 'admin.members');
}

export function canAdminMembers(member: TeamMember): boolean {
    return (isHuman(member) && isOwner(member)) || hasScope(member, 'admin.members');
}

export function canRelayPull(member: TeamMember): boolean {
    return isHuman(member) || hasScope(member, 'relay.pull');
}

export function canRelayPush(member: TeamMember): boolean {
    return isHuman(member) || hasScope(member, 'relay.push');
}

export function canRotateSelf(member: TeamMember): boolean {
    return isHuman(member) || hasScope(member, 'member.rotate.self');
}
