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
        // Import the Ed25519 public key
        const key = await crypto.subtle.importKey(
            'raw',
            publicKey,
            { name: 'Ed25519' },
            false,
            ['verify'],
        );

        // Verify the signature
        return await crypto.subtle.verify(
            'Ed25519',
            key,
            signature,
            data,
        );
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
        .map(b => b.toString(16).padStart(2, '0'))
        .join('');
}
