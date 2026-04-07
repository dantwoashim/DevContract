export type DoctorReport = {
    blocking: number;
    warnings: number;
    checks: Array<{
        name: string;
        status: string;
        detail: string;
    }>;
};

export type HealthRow = {
    label: string;
    description: string;
};

export function parseDoctorReport(raw: string): DoctorReport {
    return JSON.parse(raw.trim()) as DoctorReport;
}

export function buildHealthRows(report: DoctorReport): HealthRow[] {
    const rows: HealthRow[] = [
        { label: 'Blocking Issues', description: String(report.blocking) },
        { label: 'Warnings', description: String(report.warnings) },
    ];

    for (const check of report.checks) {
        rows.push({
            label: `${iconForStatus(check.status)} ${check.name}`,
            description: check.detail,
        });
    }

    return rows;
}

export function iconForStatus(status: string): string {
    switch (status) {
        case 'pass':
            return '$(check)';
        case 'warn':
            return '$(warning)';
        default:
            return '$(error)';
    }
}
