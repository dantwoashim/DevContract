import { createHash, webcrypto } from 'node:crypto';
import type { UnstableDevWorker } from 'wrangler';

type Identity = {
    fingerprint: string;
    publicKeyB64: string;
    privateKey: CryptoKey;
};

export async function createIdentity(label: string): Promise<Identity> {
    const keyPair = await webcrypto.subtle.generateKey(
        { name: 'Ed25519' },
        true,
        ['sign', 'verify'],
    );

    const publicKeyRaw = Buffer.from(await webcrypto.subtle.exportKey('raw', keyPair.publicKey));
    return {
        fingerprint: `SHA256:test-${label}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`,
        publicKeyB64: publicKeyRaw.toString('base64'),
        privateKey: keyPair.privateKey,
    };
}

export function transportKey(seed: number): string {
    return Buffer.alloc(32, seed).toString('base64');
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
    return signedFetch(worker, actor, `/teams/${teamId}/members/${username}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fingerprint: actor.fingerprint,
            public_key: actor.publicKeyB64,
            transport_public_key: transportPublicKey,
            role,
        }),
    });
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
