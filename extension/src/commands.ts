import * as vscode from 'vscode';
import { createDevContractTerminal, getWorkspaceFolder } from './cli';

export function registerCommands(context: vscode.ExtensionContext) {
    context.subscriptions.push(
        vscode.commands.registerCommand('devcontract.bootstrap', () => runTerminalCommand('DevContract Bootstrap', ['bootstrap'])),
        vscode.commands.registerCommand('devcontract.doctor', () => runTerminalCommand('DevContract Doctor', ['doctor'])),
        vscode.commands.registerCommand('devcontract.agentInstall', () => runTerminalCommand('DevContract Agent Install', ['agent', 'install', '--all'])),
        vscode.commands.registerCommand('devcontract.guardScan', () => runTerminalCommand('DevContract Guard', ['guard', 'scan'])),
        vscode.commands.registerCommand('devcontract.run', () => runTerminalCommand('DevContract Run', ['run'])),
        vscode.commands.registerCommand('devcontract.status', () => runTerminalCommand('DevContract Status', ['status'])),
        vscode.commands.registerCommand('devcontract.installHelp', showInstallHelp),
    );
}

function runTerminalCommand(name: string, args: string[]) {
    const cwd = getWorkspaceFolder();
    if (!cwd) {
        void vscode.window.showWarningMessage('Open a workspace folder before running DevContract commands.');
        return;
    }

    createDevContractTerminal(name, args, cwd);
}

async function showInstallHelp() {
    const platform = process.platform === 'win32'
        ? 'Windows'
        : process.platform === 'darwin'
            ? 'macOS'
            : 'Linux';
    const command = process.platform === 'win32'
        ? 'go build -o devcontract.exe ./'
        : 'go build -o devcontract ./';

    const choice = await vscode.window.showInformationMessage(
        `DevContract CLI is missing or older than this ${platform} extension expects. Build or update it from the repository root with: ${command}`,
        'Open README',
    );
    if (choice === 'Open README') {
        await vscode.env.openExternal(vscode.Uri.parse('https://github.com/dantwoashim/DevContract#quick-start'));
    }
}
