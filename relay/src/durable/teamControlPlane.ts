import { decodeBase64 } from '../middleware/auth';
import { canAdminMembers, canCreateInvite, canRotateSelf, isHuman, normalizeMember, normalizePrincipalType, normalizeScopes, normalizeTeam, ownerCount } from '../lib/principals';
import type { Env, Invite, Team, TeamAuditEvent, TeamMember, TeamMemberInput } from '../types';

type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };
type AuditInput = Omit<TeamAuditEvent, 'id' | 'team_id' | 'created_at'>;

export async function handleTeamCoordinatorRequest(state: DurableObjectState, env: Env, request: Request): Promise<Response> {
    const url = new URL(request.url);
    const teamId = resolveTeamID(request, url);

    if (request.method === 'POST' && url.pathname === '/reserve-upload') {
        const body = await request.json<{ date_key: string; limit: number }>();
        const key = `daily-count:${body.date_key}`;
        const current = Number(await state.storage.get<number>(key) || 0);
        if (current >= body.limit) {
            return json({ allowed: false, count: current, limit: body.limit }, 200);
        }
        const next = current + 1;
        await state.storage.put(key, next);
        return json({ allowed: true, count: next, limit: body.limit }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/enqueue') {
        const body = await request.json<{ recipient_fingerprint: string; blob_id: string }>();
        const key = pendingKey(body.recipient_fingerprint);
        const pending = (await state.storage.get<string[]>(key)) || [];
        if (!pending.includes(body.blob_id)) {
            pending.push(body.blob_id);
            await state.storage.put(key, pending);
        }
        return json({ pending }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/event') {
        const body = await request.json<{ event: string; delta?: number }>();
        if (!body.event) {
            return json({ error: 'missing_event' }, 400);
        }
        const delta = typeof body.delta === 'number' && body.delta > 0 ? Math.floor(body.delta) : 1;
        await recordCounterEvent(state, body.event, delta);
        return json({ status: 'recorded' }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/remove') {
        const body = await request.json<{ recipient_fingerprint: string; blob_id: string }>();
        const key = pendingKey(body.recipient_fingerprint);
        const pending = (await state.storage.get<string[]>(key)) || [];
        const filtered = pending.filter((id) => id !== body.blob_id);
        if (filtered.length === 0) {
            await state.storage.delete(key);
        } else {
            await state.storage.put(key, filtered);
        }
        return json({ pending: filtered }, 200);
    }

    if (request.method === 'GET' && url.pathname === '/pending') {
        const recipientFingerprint = url.searchParams.get('recipient_fingerprint') || '';
        const pending = (await state.storage.get<string[]>(pendingKey(recipientFingerprint))) || [];
        return json({ pending }, 200);
    }

    if (request.method === 'GET' && url.pathname === '/stats') {
        const entries = await state.storage.list<string[] | number>({ prefix: 'pending:' });
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
        const eventTotals = await readCounterPrefix(state.storage, 'event_total:');
        const eventsToday = await readCounterPrefix(state.storage, `event_day:${dateKey()}:`);
        const uploadsToday = Number(await state.storage.get<number>(uploadKey(dateKey())) || 0);
        return json({
            pending_count: pendingCount,
            pending_by_recipient: pendingByRecipient,
            event_totals: eventTotals,
            events_today: eventsToday,
            uploads_today: uploadsToday,
        }, 200);
    }

    if (!teamId) {
        return json({ error: 'missing_team_id', message: 'X-EnvSync-Team-ID header is required' }, 400);
    }

    if (request.method === 'GET' && url.pathname === '/team') {
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }
        return json({ team }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/team/bootstrap') {
        const body = await request.json<{
            founder: TeamMemberInput;
            team_name?: string;
            bootstrap_nonce_hash?: string;
            contract_hash?: string;
        }>();
        const existing = await loadTeam(state, env, teamId);
        if (existing) {
            return json({ error: 'conflict', message: 'Project already exists on the relay' }, 409);
        }

        const founder = normalizeMember({
            ...body.founder,
            role: 'owner',
            principal_type: 'human_member',
            scopes: [],
            added_at: Math.floor(Date.now() / 1000),
        } as TeamMember);
        if (normalizePrincipalType(founder.principal_type) !== 'human_member') {
            return json({ error: 'invalid_principal', message: 'Only human members can bootstrap a project' }, 400);
        }

        const team: Team = {
            id: teamId,
            name: (body.team_name || teamId).trim() || teamId,
            members: [founder],
            founded_by: founder.fingerprint,
            founding_nonce_hash: (body.bootstrap_nonce_hash || '').trim() || undefined,
            contract_hash: (body.contract_hash || '').trim() || undefined,
            created_at: Math.floor(Date.now() / 1000),
        };
        await saveTeam(state, team);
        await recordCounterEvent(state, 'team.bootstrapped', 1);
        await appendAudit(state, teamId, {
            action: 'team.bootstrapped',
            actor_fingerprint: founder.fingerprint,
            actor_principal_type: founder.principal_type,
            actor_scopes: normalizeScopes(founder.scopes),
            result: 'succeeded',
            details: `founder=${founder.username}`,
        });
        return json({ status: 'bootstrapped', member_count: 1, team }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/member/upsert') {
        const body = await request.json<{
            actor_fingerprint: string;
            member: TeamMember;
            member_limit?: number;
        }>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Project not found. Bootstrap it before adding members.' }, 404);
        }

        const actor = team.members.find((member) => member.fingerprint === body.actor_fingerprint);
        const incoming = normalizeMember(body.member);
        if (!actor) {
            return json({ error: 'forbidden', message: 'Only team members can update membership' }, 403);
        }
        if (!canAdminMembers(actor) && actor.fingerprint !== incoming.fingerprint) {
            return json({ error: 'forbidden', message: 'Only owners can add or update other members' }, 403);
        }

        const existingIdx = team.members.findIndex((member) => member.fingerprint === incoming.fingerprint);
        const usernameConflict = team.members.find((member) => member.username === incoming.username && member.fingerprint !== incoming.fingerprint);
        if (usernameConflict) {
            return json({ error: 'duplicate_username', message: `Member label ${incoming.username} is already used by another fingerprint` }, 409);
        }
        if (incoming.principal_type === 'service_principal' && incoming.role === 'owner') {
            return json({ error: 'invalid_principal', message: 'Service principals cannot be owners' }, 400);
        }
        if (existingIdx < 0 && typeof body.member_limit === 'number' && body.member_limit >= 0 && team.members.length >= body.member_limit) {
            return json({ error: 'member_limit', message: 'Team member limit reached' }, 429);
        }

        const existing = existingIdx >= 0 ? team.members[existingIdx] : undefined;
        const nextMember: TeamMember = {
            ...incoming,
            role: team.members.length === 0 ? 'owner' : (existing?.role || incoming.role || 'member'),
            added_at: existing?.added_at || Math.floor(Date.now() / 1000),
        };
        if (existingIdx >= 0) {
            if (team.members[existingIdx].role !== 'owner' && team.members[existingIdx].role !== nextMember.role && !canAdminMembers(actor)) {
                return json({ error: 'forbidden', message: 'Only owners can change member roles' }, 403);
            }
            if (team.members[existingIdx].role === 'owner' && nextMember.role !== 'owner' && ownerCount(team) <= 1) {
                return json({ error: 'owner_invariant', message: 'A project must always retain at least one human owner' }, 409);
            }
            if (team.members[existingIdx].role === 'owner') {
                nextMember.role = 'owner';
            }
            team.members[existingIdx] = nextMember;
        } else {
            team.members.push(nextMember);
        }

        await saveTeam(state, team);
        await recordCounterEvent(state, 'team.member_upserted', 1);
        await appendAudit(state, teamId, {
            action: 'team.member_upserted',
            actor_fingerprint: actor.fingerprint,
            actor_principal_type: actor.principal_type,
            actor_scopes: normalizeScopes(actor.scopes),
            target_fingerprint: nextMember.fingerprint,
            result: 'succeeded',
            details: existingIdx >= 0 ? `updated:${nextMember.username}` : `added:${nextMember.username}`,
        });
        return json({ status: existingIdx >= 0 ? 'updated' : 'added', member_count: team.members.length, member: nextMember }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/member/remove') {
        const body = await request.json<{
            actor_fingerprint: string;
            username?: string;
            fingerprint?: string;
        }>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }

        const actor = team.members.find((member) => member.fingerprint === body.actor_fingerprint);
        const target = body.fingerprint
            ? team.members.find((member) => member.fingerprint === body.fingerprint)
            : team.members.find((member) => member.username === body.username);
        if (!actor || !target) {
            return json({ error: 'not_found', message: 'Member not found in team' }, 404);
        }
        if (!canAdminMembers(actor) && actor.fingerprint !== target.fingerprint) {
            return json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
        }
        if (target.role === 'owner' && ownerCount(team) <= 1) {
            return json({ error: 'owner_invariant', message: 'A project must always retain at least one human owner' }, 409);
        }

        team.members = team.members.filter((member) => member.fingerprint !== target.fingerprint);
        await saveTeam(state, team);
        await recordCounterEvent(state, 'team.member_removed', 1);
        await appendAudit(state, teamId, {
            action: 'team.member_removed',
            actor_fingerprint: actor.fingerprint,
            actor_principal_type: actor.principal_type,
            actor_scopes: normalizeScopes(actor.scopes),
            target_fingerprint: target.fingerprint,
            result: 'succeeded',
            details: target.username,
        });
        return json({ status: 'removed', member_count: team.members.length }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/member/transfer-ownership') {
        const body = await request.json<{ actor_fingerprint: string; target_fingerprint: string }>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }

        const actorIdx = team.members.findIndex((member) => member.fingerprint === body.actor_fingerprint);
        const targetIdx = team.members.findIndex((member) => member.fingerprint === body.target_fingerprint);
        if (actorIdx < 0 || targetIdx < 0) {
            return json({ error: 'not_found', message: 'Actor or target not found in team' }, 404);
        }

        const actor = team.members[actorIdx];
        const target = team.members[targetIdx];
        if (!isHuman(actor) || actor.role !== 'owner') {
            return json({ error: 'forbidden', message: 'Only a human owner can transfer ownership' }, 403);
        }
        if (!isHuman(target)) {
            return json({ error: 'invalid_target', message: 'Ownership can only be transferred to a human member' }, 400);
        }

        team.members[actorIdx] = { ...actor, role: 'member' };
        team.members[targetIdx] = { ...target, role: 'owner' };
        await saveTeam(state, team);
        await recordCounterEvent(state, 'team.ownership_transferred', 1);
        await appendAudit(state, teamId, {
            action: 'team.ownership_transferred',
            actor_fingerprint: actor.fingerprint,
            actor_principal_type: actor.principal_type,
            actor_scopes: normalizeScopes(actor.scopes),
            target_fingerprint: target.fingerprint,
            result: 'succeeded',
            details: target.username,
        });
        return json({ status: 'transferred', owner_fingerprint: target.fingerprint }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/member/rotate-self') {
        const body = await request.json<TeamMemberInput & { actor_fingerprint: string; proof?: string }>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }

        const actorIdx = team.members.findIndex((member) => member.fingerprint === body.actor_fingerprint);
        if (actorIdx < 0) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'forbidden', message: 'Only team members can rotate identity' }, 403);
        }

        const actor = team.members[actorIdx];
        if (!canRotateSelf(actor)) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'forbidden', message: 'This principal may not rotate itself' }, 403);
        }
        if (!body.proof) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'missing_proof', message: 'proof is required for self-rotation' }, 400);
        }

        const replacement = normalizeMember({
            username: body.username,
            fingerprint: body.fingerprint,
            public_key: body.public_key,
            transport_public_key: body.transport_public_key,
            transport_fingerprint: body.transport_fingerprint,
            role: actor.role,
            principal_type: actor.principal_type,
            scopes: actor.scopes,
            added_at: actor.added_at,
        } as TeamMember);

        const conflict = team.members.find((member) => member.fingerprint === replacement.fingerprint && member.fingerprint !== actor.fingerprint);
        if (conflict) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'duplicate_fingerprint', message: 'New fingerprint is already registered on this team' }, 409);
        }
        const usernameConflict = team.members.find((member) => member.username === replacement.username && member.fingerprint !== actor.fingerprint);
        if (usernameConflict) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'duplicate_username', message: `Member label ${replacement.username} is already used by another fingerprint` }, 409);
        }

        const proofValid = await verifyRotationProof(teamId, actor.fingerprint, replacement, body.proof);
        if (!proofValid) {
            await recordCounterEvent(state, 'team.rotate_failed', 1);
            return json({ error: 'invalid_proof', message: 'Rotation proof did not verify against the replacement identity key' }, 401);
        }

        team.members[actorIdx] = replacement;
        await saveTeam(state, team);
        await recordCounterEvent(state, 'team.member_rotated', 1);
        await appendAudit(state, teamId, {
            action: 'team.member_rotated',
            actor_fingerprint: actor.fingerprint,
            actor_principal_type: actor.principal_type,
            actor_scopes: normalizeScopes(actor.scopes),
            target_fingerprint: replacement.fingerprint,
            result: 'succeeded',
            details: replacement.username,
        });
        return json({ status: 'rotated', fingerprint: replacement.fingerprint, member_count: team.members.length, member: replacement }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/invite/create') {
        const body = await request.json<Invite>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }

        const inviter = team.members.find((member) => member.fingerprint === body.inviter_fingerprint);
        if (!inviter || !canCreateInvite(inviter)) {
            return json({ error: 'forbidden', message: 'Inviter is not allowed to issue invites' }, 403);
        }

        const existing = await loadInvite(state, env, teamId, body.token_hash);
        if (existing && inviteStatus(existing) === 'pending') {
            return json({ error: 'duplicate', message: 'An invite with this token already exists' }, 409);
        }

        const invite = normalizeInvite({
            ...body,
            team_id: teamId,
            consumed: false,
        });
        await saveInvite(state, env, teamId, invite);
        await recordCounterEvent(state, 'invite.created', 1);
        await appendAudit(state, teamId, {
            action: 'invite.created',
            actor_fingerprint: inviter.fingerprint,
            actor_principal_type: inviter.principal_type,
            actor_scopes: normalizeScopes(inviter.scopes),
            invite_hash: invite.token_hash,
            result: 'succeeded',
            details: invite.invitee,
        });
        return json({ status: 'created', expires_at: invite.expires_at }, 201);
    }

    if (request.method === 'GET' && url.pathname === '/invite') {
        const tokenHash = (url.searchParams.get('token_hash') || '').trim();
        if (!tokenHash) {
            return json({ error: 'missing_invite', message: 'token_hash is required' }, 400);
        }
        const invite = await loadInvite(state, env, teamId, tokenHash);
        if (!invite) {
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }

        switch (inviteStatus(invite)) {
        case 'consumed':
            return json({ error: 'consumed', message: 'This invite has already been used' }, 410);
        case 'revoked':
            return json({ error: 'revoked', message: 'This invite is no longer valid' }, 410);
        case 'expired':
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        default:
            return json({
                team_id: invite.team_id,
                inviter: invite.inviter,
                inviter_fingerprint: invite.inviter_fingerprint,
                expires_at: invite.expires_at,
            }, 200);
        }
    }

    if (request.method === 'POST' && url.pathname === '/invite/consume') {
        const body = await request.json<{ token_hash: string; joiner_label?: string; joiner_fingerprint?: string }>();
        const invite = await loadInvite(state, env, teamId, body.token_hash);
        if (!invite) {
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }

        if (inviteStatus(invite) === 'consumed') {
            return json({ error: 'consumed', message: 'This invite has already been used' }, 410);
        }
        if (inviteStatus(invite) === 'revoked') {
            return json({ error: 'revoked', message: 'This invite is no longer valid' }, 410);
        }
        if (inviteStatus(invite) === 'expired') {
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }
        if (invite.invitee && body.joiner_label && invite.invitee !== body.joiner_label) {
            return json({ error: 'invite_mismatch', message: `Invite was issued for ${invite.invitee}, not ${body.joiner_label}` }, 409);
        }

        invite.consumed = true;
        invite.consumed_at = Math.floor(Date.now() / 1000);
        invite.consumed_by_fingerprint = (body.joiner_fingerprint || '').trim() || undefined;
        await saveInvite(state, env, teamId, invite);
        await recordCounterEvent(state, 'invite.consumed', 1);
        await appendAudit(state, teamId, {
            action: 'invite.consumed',
            actor_fingerprint: invite.consumed_by_fingerprint,
            invite_hash: invite.token_hash,
            result: 'succeeded',
            details: invite.invitee,
        });
        return json({
            status: 'consumed',
            team_id: invite.team_id,
            inviter: invite.inviter,
            inviter_fingerprint: invite.inviter_fingerprint,
            joiner_fingerprint: body.joiner_fingerprint || '',
        }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/invite/join') {
        const body = await request.json<TeamMemberInput & { token_hash: string }>();
        const invite = await loadInvite(state, env, teamId, body.token_hash);
        if (!invite) {
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }

        switch (inviteStatus(invite)) {
        case 'consumed':
            return json({ error: 'consumed', message: 'This invite has already been used' }, 410);
        case 'revoked':
            return json({ error: 'revoked', message: 'This invite is no longer valid' }, 410);
        case 'expired':
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }

        if (invite.invitee && invite.invitee !== body.username) {
            await recordCounterEvent(state, 'invite.join_failed', 1);
            await appendAudit(state, teamId, {
                action: 'invite.join_failed',
                actor_fingerprint: body.fingerprint,
                invite_hash: invite.token_hash,
                result: 'failed',
                details: `expected=${invite.invitee} got=${body.username}`,
            });
            return json({ error: 'invite_mismatch', message: `Invite was issued for ${invite.invitee}, not ${body.username}` }, 409);
        }

        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }

        const usernameConflict = team.members.find((member) => member.username === body.username && member.fingerprint !== body.fingerprint);
        if (usernameConflict) {
            await recordCounterEvent(state, 'invite.join_failed', 1);
            await appendAudit(state, teamId, {
                action: 'invite.join_failed',
                actor_fingerprint: body.fingerprint,
                target_fingerprint: usernameConflict.fingerprint,
                invite_hash: invite.token_hash,
                result: 'failed',
                details: `duplicate_username:${body.username}`,
            });
            return json({ error: 'duplicate_username', message: `Member label ${body.username} is already used by another fingerprint` }, 409);
        }

        const existingIdx = team.members.findIndex((member) => member.fingerprint === body.fingerprint);
        const existing = existingIdx >= 0 ? team.members[existingIdx] : undefined;
        const member = normalizeMember({
            username: body.username,
            fingerprint: body.fingerprint,
            public_key: body.public_key,
            transport_public_key: body.transport_public_key,
            transport_fingerprint: body.transport_fingerprint,
            role: existing?.role || 'member',
            principal_type: existing?.principal_type || 'human_member',
            scopes: existing?.scopes || [],
            added_at: existing?.added_at || Math.floor(Date.now() / 1000),
        } as TeamMember);

        if (existingIdx >= 0) {
            team.members[existingIdx] = member;
        } else {
            team.members.push(member);
        }

        invite.consumed = true;
        invite.consumed_at = Math.floor(Date.now() / 1000);
        invite.consumed_by_fingerprint = body.fingerprint;
        await saveTeam(state, team);
        await saveInvite(state, env, teamId, invite);
        await recordCounterEvent(state, 'invite.joined', 1);
        await appendAudit(state, teamId, {
            action: 'invite.joined',
            actor_fingerprint: body.fingerprint,
            actor_principal_type: member.principal_type,
            actor_scopes: normalizeScopes(member.scopes),
            invite_hash: invite.token_hash,
            result: 'succeeded',
            details: body.username,
        });
        return json({
            status: 'joined',
            team_id: invite.team_id,
            inviter: invite.inviter,
            inviter_fingerprint: invite.inviter_fingerprint,
            joiner_fingerprint: body.fingerprint,
            members: team.members.filter((member) => member.principal_type !== 'service_principal'),
        }, 200);
    }

    if (request.method === 'GET' && url.pathname === '/invites') {
        const invites = await listInvites(state);
        return json({ invites }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/invite/revoke') {
        const body = await request.json<{ actor_fingerprint: string; token_hash: string; reason?: string }>();
        const team = await loadTeam(state, env, teamId);
        if (!team) {
            return json({ error: 'not_found', message: 'Team not found' }, 404);
        }
        const actor = team.members.find((member) => member.fingerprint === body.actor_fingerprint);
        if (!actor || !canAdminMembers(actor)) {
            return json({ error: 'forbidden', message: 'Only project administrators can revoke invites' }, 403);
        }

        const invite = await loadInvite(state, env, teamId, body.token_hash);
        if (!invite) {
            return json({ error: 'not_found', message: 'Invite not found or expired' }, 404);
        }
        if (inviteStatus(invite) === 'consumed') {
            return json({ error: 'consumed', message: 'This invite has already been used' }, 410);
        }
        if (inviteStatus(invite) === 'revoked') {
            return json({ status: 'revoked', token_hash: invite.token_hash }, 200);
        }

        invite.revoked_at = Math.floor(Date.now() / 1000);
        invite.revoked_by_fingerprint = actor.fingerprint;
        invite.revoke_reason = (body.reason || '').trim() || undefined;
        await saveInvite(state, env, teamId, invite);
        await recordCounterEvent(state, 'invite.revoked', 1);
        await appendAudit(state, teamId, {
            action: 'invite.revoked',
            actor_fingerprint: actor.fingerprint,
            actor_principal_type: actor.principal_type,
            actor_scopes: normalizeScopes(actor.scopes),
            invite_hash: invite.token_hash,
            result: 'succeeded',
            details: invite.revoke_reason || invite.invitee,
        });
        return json({ status: 'revoked', token_hash: invite.token_hash }, 200);
    }

    if (request.method === 'POST' && url.pathname === '/audit') {
        const body = await request.json<AuditInput>();
        await appendAudit(state, teamId, body);
        return json({ status: 'recorded' }, 200);
    }

    if (request.method === 'GET' && url.pathname === '/audit') {
        const limit = Math.max(1, Math.min(100, Number(url.searchParams.get('limit') || 20)));
        const events = await listAudit(state, limit);
        return json({ events }, 200);
    }

    return json({ error: 'not_found' }, 404);
}

async function loadTeam(state: DurableObjectState, env: Env, teamId: string): Promise<Team | null> {
    const existing = await state.storage.get<Team>(teamStorageKey());
    if (existing) {
        return normalizeTeam(existing);
    }

    const legacy = await env.ENVSYNC_DATA.get(`team:${teamId}`);
    if (!legacy) {
        return null;
    }

    const team = normalizeTeam(JSON.parse(legacy) as Team);
    await state.storage.put(teamStorageKey(), team);
    return team;
}

async function saveTeam(state: DurableObjectState, team: Team): Promise<void> {
    await state.storage.put(teamStorageKey(), normalizeTeam(team));
}

async function loadInvite(state: DurableObjectState, env: Env, teamId: string, tokenHash: string): Promise<Invite | null> {
    const existing = await state.storage.get<Invite>(inviteStorageKey(tokenHash));
    if (existing) {
        return normalizeInvite(existing);
    }

    const legacy = await env.ENVSYNC_DATA.get(`invite:${tokenHash}`);
    if (!legacy) {
        return null;
    }

    const invite = normalizeInvite(JSON.parse(legacy) as Invite);
    if (invite.team_id !== teamId) {
        return null;
    }

    await saveInvite(state, env, teamId, invite);
    return invite;
}

async function saveInvite(state: DurableObjectState, env: Env, teamId: string, invite: Invite): Promise<void> {
    const normalized = normalizeInvite(invite);
    await state.storage.put(inviteStorageKey(normalized.token_hash), normalized);
    await env.ENVSYNC_DATA.put(inviteRefKey(normalized.token_hash), teamId, {
        expirationTtl: inviteRefTTL(normalized),
    });
}

async function listInvites(state: DurableObjectState): Promise<Invite[]> {
    const listed = await state.storage.list<Invite>({ prefix: inviteStoragePrefix() });
    return Array.from(listed.values())
        .map((invite) => normalizeInvite(invite))
        .sort((left, right) => right.created_at - left.created_at);
}

async function appendAudit(state: DurableObjectState, teamId: string, input: AuditInput): Promise<void> {
    const now = Math.floor(Date.now() / 1000);
    const seq = Number(await state.storage.get<number>(auditSequenceKey()) || 0) + 1;
    await state.storage.put(auditSequenceKey(), seq);
    const event: TeamAuditEvent = {
        id: `${now}-${seq}`,
        team_id: teamId,
        action: input.action,
        actor_fingerprint: input.actor_fingerprint,
        actor_principal_type: input.actor_principal_type,
        actor_scopes: normalizeScopes(input.actor_scopes || []),
        target_fingerprint: input.target_fingerprint,
        invite_hash: input.invite_hash,
        blob_id: input.blob_id,
        result: input.result,
        details: input.details,
        created_at: now,
    };
    await state.storage.put(auditStorageKey(now, seq), event);
}

async function listAudit(state: DurableObjectState, limit: number): Promise<TeamAuditEvent[]> {
    const listed = await state.storage.list<TeamAuditEvent>({
        prefix: auditStoragePrefix(),
        reverse: true,
        limit,
    });
    return Array.from(listed.values()).sort((left, right) => right.created_at - left.created_at);
}

async function recordCounterEvent(state: DurableObjectState, event: string, delta: number): Promise<void> {
    const totalKey = eventTotalKey(event);
    const dayKey = eventDayKey(dateKey(), event);
    const currentTotal = Number(await state.storage.get<number>(totalKey) || 0);
    const currentDay = Number(await state.storage.get<number>(dayKey) || 0);
    await state.storage.put(totalKey, currentTotal + delta);
    await state.storage.put(dayKey, currentDay + delta);
}

function resolveTeamID(request: Request, url: URL): string {
    return (request.headers.get('X-EnvSync-Team-ID') || url.searchParams.get('team_id') || '').trim();
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

function teamStorageKey(): string {
    return 'control:team';
}

function inviteStoragePrefix(): string {
    return 'control:invite:';
}

function inviteStorageKey(tokenHash: string): string {
    return `${inviteStoragePrefix()}${tokenHash}`;
}

function inviteRefKey(tokenHash: string): string {
    return `invite-ref:${tokenHash}`;
}

function auditSequenceKey(): string {
    return 'control:audit-seq';
}

function auditStoragePrefix(): string {
    return 'control:audit:';
}

function auditStorageKey(now: number, seq: number): string {
    return `${auditStoragePrefix()}${String(now).padStart(10, '0')}:${String(seq).padStart(8, '0')}`;
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

function inviteStatus(invite: Invite): Invite['status'] {
    const now = Math.floor(Date.now() / 1000);
    if (invite.revoked_at) {
        return 'revoked';
    }
    if (invite.consumed || invite.consumed_at) {
        return 'consumed';
    }
    if (invite.expires_at > 0 && invite.expires_at <= now) {
        return 'expired';
    }
    return 'pending';
}

function normalizeInvite(invite: Invite): Invite {
    return {
        ...invite,
        consumed: Boolean(invite.consumed || invite.consumed_at),
        status: inviteStatus(invite),
    };
}

function inviteRefTTL(invite: Invite): number {
    const now = Math.floor(Date.now() / 1000);
    if (invite.revoked_at || invite.consumed_at) {
        return 7 * 24 * 3600;
    }
    const ttl = invite.expires_at - now;
    if (ttl > 60) {
        return ttl;
    }
    return 24 * 3600;
}

async function verifyRotationProof(teamId: string, actorFingerprint: string, body: TeamMemberInput, proofB64: string): Promise<boolean> {
    const message = new TextEncoder().encode([
        'rotate-self',
        teamId,
        actorFingerprint,
        body.fingerprint,
        body.transport_fingerprint,
    ].join('\n'));

    const signature = decodeBase64(proofB64, 'proof');
    const publicKey = decodeBase64(body.public_key, 'public_key');
    return crypto.subtle.verify('Ed25519', await crypto.subtle.importKey('raw', publicKey, { name: 'Ed25519' }, false, ['verify']), signature, message);
}

function json(payload: Record<string, JsonValue>, status: number): Response {
    return new Response(JSON.stringify(payload), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}
