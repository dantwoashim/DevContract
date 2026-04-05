import { Hono } from 'hono';
import type { Env, BlobMetadata, Team } from '../types';
import { canUploadBlob, getBlobTtl, limitMessage } from '../middleware/tiers';

export const relayRoutes = new Hono<{ Bindings: Env }>();

relayRoutes.put('/:team/:blob', async (c) => {
    const teamId = c.req.param('team');
    const blobId = c.req.param('blob');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const maxSize = parseInt(c.env.MAX_BLOB_SIZE || '65536', 10);

    const team = await loadTeam(c.env, teamId);
    if (!team) {
        return c.json({ error: 'not_found', message: 'Team not found' }, 404);
    }

    const body = await c.req.arrayBuffer();
    if (body.byteLength > maxSize) {
        return c.json({
            error: 'too_large',
            message: `Blob exceeds maximum size of ${maxSize} bytes`,
        }, 413);
    }

    const senderFingerprint = c.req.header('X-EnvSync-Sender') || '';
    const recipientFingerprint = c.req.header('X-EnvSync-Recipient') || '';
    if (!senderFingerprint || !recipientFingerprint) {
        return c.json({ error: 'missing_headers', message: 'X-EnvSync-Sender and X-EnvSync-Recipient headers required' }, 400);
    }
    if (actorFingerprint !== senderFingerprint) {
        return c.json({ error: 'forbidden', message: 'Authenticated fingerprint must match sender' }, 403);
    }
    if (!team.members.some((member) => member.fingerprint === senderFingerprint)) {
        return c.json({ error: 'forbidden', message: 'Sender is not a team member' }, 403);
    }
    if (!team.members.some((member) => member.fingerprint === recipientFingerprint)) {
        return c.json({ error: 'forbidden', message: 'Recipient is not a team member' }, 403);
    }

    const rateLimitKey = `ratelimit:blob:${teamId}:${dateKey()}`;
    const currentCount = parseInt(await c.env.ENVSYNC_DATA.get(rateLimitKey) || '0', 10);
    if (!(await canUploadBlob(c.env, teamId, currentCount))) {
        return c.json({
            error: 'rate_limited',
            message: limitMessage('Relay blob limit reached'),
        }, 429);
    }

    const ttlSeconds = await getBlobTtl(c.env, teamId);

    const metadata: BlobMetadata = {
        blob_id: blobId,
        team_id: teamId,
        sender_fingerprint: senderFingerprint,
        recipient_fingerprint: recipientFingerprint,
        ephemeral_public_key: c.req.header('X-EnvSync-EphemeralKey') || '',
        sender_signature: c.req.header('X-EnvSync-Signature') || '',
        size: body.byteLength,
        uploaded_at: Math.floor(Date.now() / 1000),
        expires_at: Math.floor(Date.now() / 1000) + ttlSeconds,
        filename: c.req.header('X-EnvSync-Filename') || '.env',
    };

    await c.env.ENVSYNC_DATA.put(`blob:${teamId}:${blobId}:data`, body, { expirationTtl: ttlSeconds });
    await c.env.ENVSYNC_DATA.put(`blob:${teamId}:${blobId}:meta`, JSON.stringify(metadata), { expirationTtl: ttlSeconds });

    const pendingKey = `pending:${teamId}:${recipientFingerprint}`;
    const pendingList = JSON.parse(await c.env.ENVSYNC_DATA.get(pendingKey) || '[]') as string[];
    if (!pendingList.includes(blobId)) {
        pendingList.push(blobId);
        await c.env.ENVSYNC_DATA.put(pendingKey, JSON.stringify(pendingList), { expirationTtl: ttlSeconds });
    }

    await c.env.ENVSYNC_DATA.put(rateLimitKey, String(currentCount + 1), { expirationTtl: 86400 });
    return c.json({ status: 'stored', blob_id: blobId, expires_at: metadata.expires_at }, 201);
});

relayRoutes.get('/:team/pending', async (c) => {
    const teamId = c.req.param('team');
    const actorFingerprint = c.get('fingerprint' as never) as string;
    const requestedFingerprint = c.req.query('for') || actorFingerprint;

    if (requestedFingerprint !== actorFingerprint) {
        return c.json({ error: 'forbidden', message: 'Can only list pending blobs for the authenticated fingerprint' }, 403);
    }

    const team = await loadTeam(c.env, teamId);
    if (!team || !team.members.some((member) => member.fingerprint === actorFingerprint)) {
        return c.json({ error: 'forbidden', message: 'Only team members can list pending blobs' }, 403);
    }

    const pendingKey = `pending:${teamId}:${requestedFingerprint}`;
    const pendingList = JSON.parse(await c.env.ENVSYNC_DATA.get(pendingKey) || '[]') as string[];

    const blobs: BlobMetadata[] = [];
    for (const blobId of pendingList) {
        const metaData = await c.env.ENVSYNC_DATA.get(`blob:${teamId}:${blobId}:meta`);
        if (metaData) {
            blobs.push(JSON.parse(metaData));
        }
    }

    return c.json({ pending: blobs });
});

relayRoutes.get('/:team/:blob', async (c) => {
    const teamId = c.req.param('team');
    const blobId = c.req.param('blob');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const metaData = await c.env.ENVSYNC_DATA.get(`blob:${teamId}:${blobId}:meta`);
    if (!metaData) {
        return c.json({ error: 'not_found', message: 'Blob not found or expired' }, 404);
    }

    const metadata: BlobMetadata = JSON.parse(metaData);
    if (metadata.recipient_fingerprint !== actorFingerprint) {
        return c.json({ error: 'forbidden', message: 'Only the intended recipient may download this blob' }, 403);
    }

    const data = await c.env.ENVSYNC_DATA.get(`blob:${teamId}:${blobId}:data`, 'arrayBuffer');
    if (!data) {
        return c.json({ error: 'not_found', message: 'Blob data not found' }, 404);
    }

    return new Response(data, {
        headers: {
            'Content-Type': 'application/octet-stream',
            'X-EnvSync-Sender': metadata.sender_fingerprint,
            'X-EnvSync-EphemeralKey': metadata.ephemeral_public_key,
            'X-EnvSync-Filename': metadata.filename,
            'X-EnvSync-UploadedAt': String(metadata.uploaded_at),
            'X-EnvSync-Signature': metadata.sender_signature,
        },
    });
});

relayRoutes.delete('/:team/:blob', async (c) => {
    const teamId = c.req.param('team');
    const blobId = c.req.param('blob');
    const actorFingerprint = c.get('fingerprint' as never) as string;

    const metaData = await c.env.ENVSYNC_DATA.get(`blob:${teamId}:${blobId}:meta`);
    if (!metaData) {
        return c.json({ status: 'deleted' });
    }

    const metadata: BlobMetadata = JSON.parse(metaData);
    if (metadata.recipient_fingerprint !== actorFingerprint) {
        return c.json({ error: 'forbidden', message: 'Only the intended recipient may delete this blob' }, 403);
    }

    await c.env.ENVSYNC_DATA.delete(`blob:${teamId}:${blobId}:data`);
    await c.env.ENVSYNC_DATA.delete(`blob:${teamId}:${blobId}:meta`);

    const pendingKey = `pending:${teamId}:${actorFingerprint}`;
    const pendingList = JSON.parse(await c.env.ENVSYNC_DATA.get(pendingKey) || '[]') as string[];
    const updated = pendingList.filter((id) => id !== blobId);
    if (updated.length > 0) {
        await c.env.ENVSYNC_DATA.put(pendingKey, JSON.stringify(updated));
    } else {
        await c.env.ENVSYNC_DATA.delete(pendingKey);
    }

    return c.json({ status: 'deleted' });
});

async function loadTeam(env: Env, teamId: string): Promise<Team | null> {
    const data = await env.ENVSYNC_DATA.get(`team:${teamId}`);
    if (!data) {
        return null;
    }
    return JSON.parse(data) as Team;
}

function dateKey(): string {
    return new Date().toISOString().split('T')[0];
}
