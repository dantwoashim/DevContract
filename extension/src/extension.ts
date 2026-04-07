import * as vscode from 'vscode';
import { registerCommands } from './commands';
import { execEnvSync, getWorkspaceFolder } from './cli';
import { createStatusBar, setStatusBarState } from './statusbar';
import { registerSidebar } from './sidebar';

export async function activate(context: vscode.ExtensionContext) {
    registerCommands(context);

    const statusBarItem = createStatusBar(context);
    registerSidebar(context);

    const refresh = async () => {
        const workspace = getWorkspaceFolder();
        if (!workspace) {
            setStatusBarState(statusBarItem, {
                icon: 'warning',
                text: 'No Workspace',
                tooltip: 'Open a repository to use EnvSync.',
            });
            return;
        }

        try {
            await execEnvSync(['version', '--short'], { cwd: workspace, timeout: 5000 });
            setStatusBarState(statusBarItem, {
                icon: 'check',
                text: 'Ready',
                tooltip: 'EnvSync CLI detected. Open the EnvSync activity view for onboarding actions.',
                command: 'envsync.showQuickPick',
            });
        } catch (error) {
            setStatusBarState(statusBarItem, {
                icon: 'warning',
                text: 'CLI Missing',
                tooltip: `EnvSync CLI not available: ${error instanceof Error ? error.message : String(error)}. Click for install guidance.`,
                command: 'envsync.installHelp',
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
