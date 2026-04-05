import { Hono } from 'hono';
import type { Env, Team, TeamMember } from '../types';
import { canAddMember, limitMessage } from '../middleware/tiers';

export const teamRoutes = new Hono<{ Bindings: Env }>();

teamRoutes.get('/:team/members', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ members: [] });
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    if (!actor) {
        return c.json({ error: 'forbidden', message: 'Only team members can list members' }, 403);
    }

    return c.json({ members: team.members });
});

teamRoutes.put('/:team/members/:user', async (c) => {
    const teamId = c.req.param('team');
    const username = c.req.param('user');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const body = await c.req.json<{
        fingerprint: string;
        public_key: string;
        transport_public_key: string;
        role?: 'owner' | 'member';
    }>();

    if (!body.fingerprint || !body.public_key || !body.transport_public_key) {
        return c.json({ error: 'missing_fields', message: 'fingerprint, public_key, and transport_public_key are required' }, 400);
    }

    let team = await loadTeam(c.env, teamId);
    if (!team) {
        if (actorFingerprint !== body.fingerprint) {
            return c.json({ error: 'forbidden', message: 'Only the authenticated device may bootstrap a team' }, 403);
        }

        team = {
            id: teamId,
            name: teamId,
            members: [],
            created_at: Math.floor(Date.now() / 1000),
        };
    } else {
        const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
        if (!actor) {
            return c.json({ error: 'forbidden', message: 'Only team members can update membership' }, 403);
        }
        if (actor.role !== 'owner' && actorFingerprint !== body.fingerprint) {
            return c.json({ error: 'forbidden', message: 'Only owners can add or update other members' }, 403);
        }
    }

    const existingIdx = team.members.findIndex((member) => member.username === username);
    if (existingIdx < 0 && !(await canAddMember(c.env, teamId, team.members.length))) {
        return c.json({
            error: 'member_limit',
            message: limitMessage('Team member limit reached'),
        }, 429);
    }

    const member: TeamMember = {
        username,
        fingerprint: body.fingerprint,
        public_key: body.public_key,
        transport_public_key: body.transport_public_key,
        transport_fingerprint: '',
        role: team.members.length === 0 ? 'owner' : (body.role || 'member'),
        added_at: Math.floor(Date.now() / 1000),
    };

    if (existingIdx >= 0) {
        team.members[existingIdx] = member;
    } else {
        team.members.push(member);
    }

    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));
    return c.json({ status: 'added', member_count: team.members.length });
});

teamRoutes.delete('/:team/members/:user', async (c) => {
    const teamId = c.req.param('team');
    const username = c.req.param('user');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const actor = team.members.find((member) => member.fingerprint === actorFingerprint);
    const target = team.members.find((member) => member.username === username);
    if (!actor || !target) {
        return c.json({ error: 'not_found', message: `User @${username} not in team` }, 404);
    }
    if (actor.role !== 'owner' && actor.fingerprint !== target.fingerprint) {
        return c.json({ error: 'forbidden', message: 'Only owners can remove other members' }, 403);
    }

    team.members = team.members.filter((member) => member.username !== username);
    await c.env.ENVSYNC_DATA.put(`team:${teamId}`, JSON.stringify(team));

    return c.json({ status: 'removed', member_count: team.members.length });
});

async function loadTeam(env: Env, teamId: string): Promise<Team | null> {
    const data = await env.ENVSYNC_DATA.get(`team:${teamId}`);
    if (!data) {
        return null;
    }
    return JSON.parse(data) as Team;
}
