import { createHash, webcrypto } from 'node:crypto';
import { unstable_dev, type UnstableDevWorker } from 'wrangler';

type Identity = {
    fingerprint: string;
    publicKeyB64: string;
    privateKey: CryptoKey;
    label: string;
};

export async function startTestWorker(): Promise<UnstableDevWorker> {
    return unstable_dev('src/index.ts', {
        experimental: {
            disableExperimentalWarning: true,
            disableDevRegistry: true,
            testMode: true,
            watch: false,
        },
        vars: {},
    });
}

export async function createIdentity(label: string): Promise<Identity> {
    const keyPair = await webcrypto.subtle.generateKey(
        { name: 'Ed25519' },
        true,
        ['sign', 'verify'],
    );

    const publicKeyRaw = Buffer.from(await webcrypto.subtle.exportKey('raw', keyPair.publicKey));
    return {
        label,
        fingerprint: computeIdentityFingerprint(publicKeyRaw),
        publicKeyB64: publicKeyRaw.toString('base64'),
        privateKey: keyPair.privateKey,
    };
}

export function transportKey(seed: number): string {
    return Buffer.alloc(32, seed).toString('base64');
}

export function transportFingerprint(publicKeyB64: string): string {
    const digest = createHash('sha256').update(Buffer.from(publicKeyB64, 'base64')).digest('base64');
    return `SHA256:${digest.replace(/=+$/g, '')}`;
}

export async function signedFetch(
    worker: UnstableDevWorker,
    identity: Identity,
    path: string,
    init: RequestInit = {},
) {
    const method = init.method || 'GET';
    const bodyBytes = normalizeBody(init.body);
    const timestamp = Math.floor(Date.now() / 1000);
    const bodyHash = createHash('sha256').update(bodyBytes).digest('hex');
    const pathname = new URL(`https://envsync.test${path}`).pathname;
    const payload = Buffer.from(`${method}\n${pathname}\n${timestamp}\n${bodyHash}`);
    const signature = Buffer.from(await webcrypto.subtle.sign('Ed25519', identity.privateKey, payload));

    const headers = new Headers(init.headers || {});
    headers.set(
        'Authorization',
        `ES-SIG timestamp=${timestamp},fingerprint=${identity.fingerprint},signature=${signature.toString('base64')},public_key=${identity.publicKeyB64}`,
    );

    return worker.fetch(path, {
        ...init,
        headers,
    });
}

export async function registerMember(
    worker: UnstableDevWorker,
    actor: Identity,
    teamId: string,
    username: string,
    transportPublicKey: string,
    role: 'owner' | 'member' = 'member',
) {
    if (role === 'owner') {
        return signedFetch(worker, actor, `/teams/${teamId}/bootstrap`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username,
                fingerprint: actor.fingerprint,
                public_key: actor.publicKeyB64,
                transport_public_key: transportPublicKey,
                transport_fingerprint: transportFingerprint(transportPublicKey),
                role: 'owner',
                team_name: teamId,
                bootstrap_nonce: `nonce-${teamId}-${username}`,
            }),
        });
    }

    return signedFetch(worker, actor, `/teams/${teamId}/members/${username}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fingerprint: actor.fingerprint,
            public_key: actor.publicKeyB64,
            transport_public_key: transportPublicKey,
            transport_fingerprint: transportFingerprint(transportPublicKey),
            role,
        }),
    });
}

export async function rotationProof(identity: Identity, teamId: string, oldFingerprint: string, newFingerprint: string, newTransportFingerprint: string): Promise<string> {
    const payload = Buffer.from([
        'rotate-self',
        teamId,
        oldFingerprint,
        newFingerprint,
        newTransportFingerprint,
    ].join('\n'));
    const signature = Buffer.from(await webcrypto.subtle.sign('Ed25519', identity.privateKey, payload));
    return signature.toString('base64');
}

function computeIdentityFingerprint(publicKeyRaw: Buffer): string {
    const typeName = Buffer.from('ssh-ed25519');
    const wire = Buffer.alloc(4 + typeName.length + 4 + publicKeyRaw.length);
    wire.writeUInt32BE(typeName.length, 0);
    typeName.copy(wire, 4);
    wire.writeUInt32BE(publicKeyRaw.length, 4 + typeName.length);
    publicKeyRaw.copy(wire, 8 + typeName.length);

    const digest = createHash('sha256').update(wire).digest('base64');
    return `SHA256:${digest.replace(/=+$/g, '')}`;
}

function normalizeBody(body: RequestInit['body'] | undefined): Buffer {
    if (!body) {
        return Buffer.alloc(0);
    }
    if (typeof body === 'string') {
        return Buffer.from(body);
    }
    if (body instanceof Uint8Array) {
        return Buffer.from(body);
    }
    if (body instanceof ArrayBuffer) {
        return Buffer.from(body);
    }
    return Buffer.from(String(body));
}
