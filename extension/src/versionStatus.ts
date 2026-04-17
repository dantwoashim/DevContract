export type CliVersionAssessment = {
    kind: 'ready' | 'outdated';
    version: string;
    minimumVersion?: string;
};

export function assessCliVersion(cliVersion: string, minimumVersion: string): CliVersionAssessment {
    const normalizedCliVersion = normalizeVersion(cliVersion);
    const normalizedMinimumVersion = normalizeVersion(minimumVersion);

    if (normalizedCliVersion === '' || normalizedMinimumVersion === '' || isDevelopmentVersion(normalizedCliVersion)) {
        return {
            kind: 'ready',
            version: normalizedCliVersion,
        };
    }

    const cliSemver = parseSemver(normalizedCliVersion);
    const minimumSemver = parseSemver(normalizedMinimumVersion);
    if (!cliSemver || !minimumSemver) {
        return {
            kind: 'ready',
            version: normalizedCliVersion,
        };
    }

    if (compareSemver(cliSemver, minimumSemver) < 0) {
        return {
            kind: 'outdated',
            version: normalizedCliVersion,
            minimumVersion: normalizedMinimumVersion,
        };
    }

    return {
        kind: 'ready',
        version: normalizedCliVersion,
    };
}

type Semver = {
    major: number;
    minor: number;
    patch: number;
};

function normalizeVersion(value: string): string {
    return String(value || '').trim().replace(/^v/i, '');
}

function isDevelopmentVersion(value: string): boolean {
    const lower = value.toLowerCase();
    return lower === 'dev' || lower.startsWith('dev-') || lower.includes('-dev') || lower.includes('+dev');
}

function parseSemver(value: string): Semver | null {
    const match = value.match(/^(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/);
    if (!match) {
        return null;
    }

    return {
        major: Number(match[1]),
        minor: Number(match[2]),
        patch: Number(match[3]),
    };
}

function compareSemver(left: Semver, right: Semver): number {
    if (left.major !== right.major) {
        return left.major - right.major;
    }
    if (left.minor !== right.minor) {
        return left.minor - right.minor;
    }
    return left.patch - right.patch;
}
