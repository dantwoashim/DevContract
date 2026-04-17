import * as vscode from 'vscode';
import { registerCommands } from './commands';
import { execDevContract, getWorkspaceFolder } from './cli';
import { createStatusBar, setStatusBarState } from './statusbar';
import { registerSidebar } from './sidebar';
import { assessCliVersion } from './versionStatus';

export async function activate(context: vscode.ExtensionContext) {
    registerCommands(context);

    const statusBarItem = createStatusBar(context);
    const extensionVersion = String(context.extension.packageJSON.version || '');
    registerSidebar(context, extensionVersion);

    const refresh = async () => {
        const workspace = getWorkspaceFolder();
        if (!workspace) {
            setStatusBarState(statusBarItem, {
                icon: 'warning',
                text: 'No Workspace',
                tooltip: 'Open a repository to use DevContract.',
            });
            return;
        }

        try {
            const cliVersion = await execDevContract(['version', '--short'], { cwd: workspace, timeout: 5000 });
            const assessment = assessCliVersion(cliVersion, extensionVersion);
            if (assessment.kind === 'outdated') {
                setStatusBarState(statusBarItem, {
                    icon: 'warning',
                    text: 'CLI Outdated',
                    tooltip: `DevContract CLI ${assessment.version} is older than this extension expects (${assessment.minimumVersion}+). Click for update guidance.`,
                    command: 'devcontract.installHelp',
                });
                return;
            }

            setStatusBarState(statusBarItem, {
                icon: 'check',
                text: 'Ready',
                tooltip: `DevContract CLI ${assessment.version || 'detected'}. Open the DevContract activity view for onboarding actions.`,
                command: 'devcontract.showQuickPick',
            });
        } catch (error) {
            setStatusBarState(statusBarItem, {
                icon: 'warning',
                text: 'CLI Missing',
                tooltip: `DevContract CLI not available: ${error instanceof Error ? error.message : String(error)}. Click for install guidance.`,
                command: 'devcontract.installHelp',
            });
        }
    };

    await refresh();
    const timer = setInterval(() => {
        void refresh();
    }, 30000);
    context.subscriptions.push({ dispose: () => clearInterval(timer) });
}

export function deactivate() {
    return undefined;
}
