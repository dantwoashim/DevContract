import * as vscode from 'vscode';
import { execDevContractCapture, getWorkspaceFolder } from './cli';
import { buildHealthRows, parseDoctorReport } from './doctorView';
import { assessCliVersion } from './versionStatus';

export function registerSidebar(context: vscode.ExtensionContext, extensionVersion: string) {
    const actionsProvider = new ActionsProvider();
    const healthProvider = new HealthProvider(extensionVersion);

    context.subscriptions.push(
        vscode.window.registerTreeDataProvider('devcontract.actions', actionsProvider),
        vscode.window.registerTreeDataProvider('devcontract.health', healthProvider),
    );

    const timer = setInterval(() => healthProvider.refresh(), 30000);
    context.subscriptions.push({ dispose: () => clearInterval(timer) });
}

class ActionsProvider implements vscode.TreeDataProvider<DevContractItem> {
    getTreeItem(element: DevContractItem): vscode.TreeItem {
        return element;
    }

    getChildren(): DevContractItem[] {
        return [
            new DevContractItem('Bootstrap Repo', 'Scaffold local outputs and run setup', 'devcontract.bootstrap'),
            new DevContractItem('Run Doctor', 'Check repo health and onboarding prerequisites', 'devcontract.doctor'),
            new DevContractItem('Install Tool Files', 'Generate instruction files and JSON tool config', 'devcontract.agentInstall'),
            new DevContractItem('Run Guard Scan', 'Scan instruction files and config for secrets', 'devcontract.guardScan'),
            new DevContractItem('Run Default Target', 'Execute the contract default workflow', 'devcontract.run'),
            new DevContractItem('Show Status', 'Inspect current project sync status', 'devcontract.status'),
        ];
    }
}

class HealthProvider implements vscode.TreeDataProvider<DevContractItem> {
    private readonly emitter = new vscode.EventEmitter<void>();
    readonly onDidChangeTreeData = this.emitter.event;

    constructor(private readonly extensionVersion: string) {}

    refresh() {
        this.emitter.fire();
    }

    getTreeItem(element: DevContractItem): vscode.TreeItem {
        return element;
    }

    async getChildren(): Promise<DevContractItem[]> {
        const cwd = getWorkspaceFolder();
        if (!cwd) {
            return [new DevContractItem('Open a workspace folder', 'DevContract health is repo-scoped')];
        }

        try {
            const versionResult = await execDevContractCapture(['version', '--short'], {
                cwd,
                timeout: 5000,
            });
            if (versionResult.exitCode !== 0) {
                throw new Error(versionResult.stderr || versionResult.stdout || `devcontract exited with status ${versionResult.exitCode}`);
            }
            const assessment = assessCliVersion(versionResult.stdout || versionResult.stderr, this.extensionVersion);
            if (assessment.kind === 'outdated') {
                return [
                    new DevContractItem(
                        'CLI outdated',
                        `DevContract CLI ${assessment.version} is older than this extension expects (${assessment.minimumVersion}+).`,
                    ),
                ];
            }
        } catch (error) {
            return [
                new DevContractItem(
                    'CLI unavailable',
                    error instanceof Error ? error.message : String(error),
                ),
            ];
        }

        try {
            const result = await execDevContractCapture(['doctor', '--json', '--quiet'], {
                cwd,
                timeout: 20000,
            });
            const report = parseDoctorReport(result.stdout || result.stderr);
            return buildHealthRows(report).map((row) => new DevContractItem(row.label, row.description));
        } catch (error) {
            return [
                new DevContractItem(
                    'Doctor unavailable',
                    error instanceof Error ? error.message : String(error),
                ),
            ];
        }
    }
}

class DevContractItem extends vscode.TreeItem {
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
