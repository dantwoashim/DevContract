import type { Env } from '../types';

/**
 * Ed25519 signature verification middleware.
 *
 * Expected Authorization header format:
 * ES-SIG timestamp=<unix>,fingerprint=<fp>,signature=<base64>
 *
 * The signature covers: {method}\n{path}\n{timestamp}\n{body_sha256}
 */
export async function verifySignature(
    method: string,
    path: string,
    timestamp: number,
    bodyHash: string,
    signature: Uint8Array,
    publicKey: Uint8Array,
): Promise<boolean> {
    const payload = `${method}\n${path}\n${timestamp}\n${bodyHash}`;
    const encoder = new TextEncoder();
    const data = encoder.encode(payload);

    try {
        const key = await crypto.subtle.importKey(
            'raw',
            publicKey,
            { name: 'Ed25519' },
            false,
            ['verify'],
        );

        return await crypto.subtle.verify('Ed25519', key, signature, data);
    } catch {
        return false;
    }
}

/**
 * Parse the ES-SIG authorization header.
 */
export function parseAuthHeader(header: string): {
    timestamp: number;
    fingerprint: string;
    signature: string;
    publicKey?: string;
} | null {
    if (!header.startsWith('ES-SIG ')) {
        return null;
    }

    const params = new Map<string, string>();
    const parts = header.slice(7).split(',');

    for (const part of parts) {
        const eqIdx = part.indexOf('=');
        if (eqIdx > 0) {
            params.set(part.slice(0, eqIdx).trim(), part.slice(eqIdx + 1).trim());
        }
    }

    const timestamp = parseInt(params.get('timestamp') || '0');
    const fingerprint = params.get('fingerprint') || '';
    const signature = params.get('signature') || '';
    const publicKey = params.get('public_key') || undefined;

    if (!timestamp || !fingerprint || !signature) {
        return null;
    }

    return { timestamp, fingerprint, signature, publicKey };
}

/**
 * Compute SHA-256 hash of a body buffer.
 */
export async function hashBody(body: ArrayBuffer): Promise<string> {
    const hash = await crypto.subtle.digest('SHA-256', body);
    return Array.from(new Uint8Array(hash))
        .map((b) => b.toString(16).padStart(2, '0'))
        .join('');
}

export function decodeBase64(value: string, label: string): Uint8Array {
    try {
        return Uint8Array.from(atob(value), (ch: string) => ch.charCodeAt(0));
    } catch {
        throw new Error(`invalid base64 for ${label}`);
    }
}

export async function computeIdentityFingerprint(publicKey: Uint8Array): Promise<string> {
    if (publicKey.length !== 32) {
        throw new Error(`invalid Ed25519 public key length: ${publicKey.length}`);
    }

    const typeName = new TextEncoder().encode('ssh-ed25519');
    const wire = new Uint8Array(4 + typeName.length + 4 + publicKey.length);
    const view = new DataView(wire.buffer);
    view.setUint32(0, typeName.length);
    wire.set(typeName, 4);
    view.setUint32(4 + typeName.length, publicKey.length);
    wire.set(publicKey, 8 + typeName.length);

    const digest = await crypto.subtle.digest('SHA-256', wire);
    return `SHA256:${base64Raw(new Uint8Array(digest))}`;
}

export async function computeTransportFingerprint(publicKey: Uint8Array): Promise<string> {
    if (publicKey.length !== 32) {
        throw new Error(`invalid transport public key length: ${publicKey.length}`);
    }

    const digest = await crypto.subtle.digest('SHA-256', publicKey);
    return `SHA256:${base64Raw(new Uint8Array(digest))}`;
}

export async function getStoredPublicKey(env: Env, fingerprint: string): Promise<string | null> {
    return env.DEVCONTRACT_DATA.get(`pubkey:${fingerprint}`);
}

function base64Raw(data: Uint8Array): string {
    const base64 = btoa(String.fromCharCode(...data));
    return base64.replace(/=+$/g, '');
}
