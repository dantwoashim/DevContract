import * as vscode from 'vscode';

type StatusBarState = {
    icon: 'check' | 'warning';
    text: string;
    tooltip: string;
    command?: string;
};

export function createStatusBar(context: vscode.ExtensionContext): vscode.StatusBarItem {
    const item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    item.command = 'envsync.showQuickPick';

    context.subscriptions.push(
        vscode.commands.registerCommand('envsync.showQuickPick', showQuickPick),
    );

    setStatusBarState(item, {
        icon: 'warning',
        text: 'Loading',
        tooltip: 'Checking EnvSync CLI availability...',
    });

    item.show();
    context.subscriptions.push(item);
    return item;
}

export function setStatusBarState(item: vscode.StatusBarItem, state: StatusBarState) {
    item.text = `$(${state.icon}) EnvSync ${state.text}`;
    item.tooltip = state.tooltip;
    item.color = state.icon === 'check' ? '#10B981' : '#F59E0B';
    item.command = state.command || 'envsync.showQuickPick';
}

async function showQuickPick() {
    const items: Array<vscode.QuickPickItem & { command: string }> = [
        { label: '$(play) Bootstrap', description: 'Prepare the repo from .envsync/contract.yaml', command: 'envsync.bootstrap' },
        { label: '$(pulse) Doctor', description: 'Validate runtimes, services, generated files, and guard state', command: 'envsync.doctor' },
        { label: '$(tools) Install Tool Files', description: 'Generate instruction files and JSON tool config', command: 'envsync.agentInstall' },
        { label: '$(shield) Guard Scan', description: 'Scan instruction files and config for leaked secrets', command: 'envsync.guardScan' },
        { label: '$(terminal) Run Default Target', description: 'Execute the repo contract default run target', command: 'envsync.run' },
        { label: '$(info) Status', description: 'Show current project sync status', command: 'envsync.status' },
    ];

    const selected = await vscode.window.showQuickPick(items, {
        placeHolder: 'Choose an EnvSync workflow',
    });

    if (selected) {
        await vscode.commands.executeCommand(selected.command);
    }
}
