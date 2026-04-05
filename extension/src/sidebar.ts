import * as vscode from 'vscode';
import { execEnvSync, getWorkspaceFolder } from './cli';

type DoctorReport = {
    blocking: number;
    warnings: number;
    checks: Array<{
        name: string;
        status: string;
        detail: string;
    }>;
};

export function registerSidebar(context: vscode.ExtensionContext) {
    const actionsProvider = new ActionsProvider();
    const healthProvider = new HealthProvider();

    context.subscriptions.push(
        vscode.window.registerTreeDataProvider('envsync.actions', actionsProvider),
        vscode.window.registerTreeDataProvider('envsync.health', healthProvider),
    );

    const timer = setInterval(() => healthProvider.refresh(), 30000);
    context.subscriptions.push({ dispose: () => clearInterval(timer) });
}

class ActionsProvider implements vscode.TreeDataProvider<EnvSyncItem> {
    getTreeItem(element: EnvSyncItem): vscode.TreeItem {
        return element;
    }

    getChildren(): EnvSyncItem[] {
        return [
            new EnvSyncItem('Bootstrap Repo', 'Scaffold local outputs and run setup', 'envsync.bootstrap'),
            new EnvSyncItem('Run Doctor', 'Check repo health and onboarding prerequisites', 'envsync.doctor'),
            new EnvSyncItem('Install Tool Files', 'Generate instruction files and JSON tool config', 'envsync.agentInstall'),
            new EnvSyncItem('Run Guard Scan', 'Scan instruction files and config for secrets', 'envsync.guardScan'),
            new EnvSyncItem('Run Default Target', 'Execute the contract default workflow', 'envsync.run'),
            new EnvSyncItem('Show Status', 'Inspect current project sync status', 'envsync.status'),
        ];
    }
}

class HealthProvider implements vscode.TreeDataProvider<EnvSyncItem> {
    private readonly emitter = new vscode.EventEmitter<void>();
    readonly onDidChangeTreeData = this.emitter.event;

    refresh() {
        this.emitter.fire();
    }

    getTreeItem(element: EnvSyncItem): vscode.TreeItem {
        return element;
    }

    async getChildren(): Promise<EnvSyncItem[]> {
        const cwd = getWorkspaceFolder();
        if (!cwd) {
            return [new EnvSyncItem('Open a workspace folder', 'EnvSync health is repo-scoped')];
        }

        try {
            const output = await execEnvSync(['doctor', '--json', '--skip-relay', '--quiet'], {
                cwd,
                timeout: 15000,
            });
            const report = JSON.parse(output) as DoctorReport;
            const items: EnvSyncItem[] = [];
            items.push(new EnvSyncItem('Blocking Issues', String(report.blocking)));
            items.push(new EnvSyncItem('Warnings', String(report.warnings)));

            for (const check of report.checks.slice(0, 6)) {
                items.push(new EnvSyncItem(`${iconForStatus(check.status)} ${check.name}`, check.detail));
            }

            return items;
        } catch (error) {
            return [
                new EnvSyncItem(
                    'Doctor unavailable',
                    error instanceof Error ? error.message : String(error),
                ),
            ];
        }
    }
}

class EnvSyncItem extends vscode.TreeItem {
    constructor(label: string, description: string, command?: string) {
        super(label, vscode.TreeItemCollapsibleState.None);
        this.description = description;
        if (command) {
            this.command = {
                command,
                title: label,
            };
        }
    }
}

function iconForStatus(status: string): string {
    switch (status) {
        case 'pass':
            return '$(check)';
        case 'warn':
            return '$(warning)';
        default:
            return '$(error)';
    }
}
