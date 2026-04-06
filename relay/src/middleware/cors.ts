import type { Env } from '../types';

export function allowedOrigin(env: Env): string {
    const configured = (env.CORS_ALLOW_ORIGIN || '').trim();
    if (configured) {
        return configured;
    }
    if ((env.ENVIRONMENT || '').toLowerCase() === 'production') {
        return 'https://relay.envsync.dev';
    }
    return '*';
}
