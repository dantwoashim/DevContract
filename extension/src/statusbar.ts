import * as vscode from 'vscode';

type StatusBarState = {
    icon: 'check' | 'warning';
    text: string;
    tooltip: string;
    command?: string;
};

export function createStatusBar(context: vscode.ExtensionContext): vscode.StatusBarItem {
    const item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    item.command = 'devcontract.showQuickPick';

    context.subscriptions.push(
        vscode.commands.registerCommand('devcontract.showQuickPick', showQuickPick),
    );

    setStatusBarState(item, {
        icon: 'warning',
        text: 'Loading',
        tooltip: 'Checking DevContract CLI availability...',
    });

    item.show();
    context.subscriptions.push(item);
    return item;
}

export function setStatusBarState(item: vscode.StatusBarItem, state: StatusBarState) {
    item.text = `$(${state.icon}) DevContract ${state.text}`;
    item.tooltip = state.tooltip;
    item.color = state.icon === 'check' ? '#10B981' : '#F59E0B';
    item.command = state.command || 'devcontract.showQuickPick';
}

async function showQuickPick() {
    const items: Array<vscode.QuickPickItem & { command: string }> = [
        { label: '$(play) Bootstrap', description: 'Prepare the repo from .devcontract/contract.yaml', command: 'devcontract.bootstrap' },
        { label: '$(pulse) Doctor', description: 'Validate runtimes, services, generated files, and guard state', command: 'devcontract.doctor' },
        { label: '$(tools) Install Tool Files', description: 'Generate instruction files and JSON tool config', command: 'devcontract.agentInstall' },
        { label: '$(shield) Guard Scan', description: 'Scan instruction files and config for leaked secrets', command: 'devcontract.guardScan' },
        { label: '$(terminal) Run Default Target', description: 'Execute the repo contract default run target', command: 'devcontract.run' },
        { label: '$(info) Status', description: 'Show current project sync status', command: 'devcontract.status' },
    ];

    const selected = await vscode.window.showQuickPick(items, {
        placeHolder: 'Choose an DevContract workflow',
    });

    if (selected) {
        await vscode.commands.executeCommand(selected.command);
    }
}
