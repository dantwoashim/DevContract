import * as vscode from 'vscode';
import { createEnvSyncTerminal, getWorkspaceFolder } from './cli';

export function registerCommands(context: vscode.ExtensionContext) {
    context.subscriptions.push(
        vscode.commands.registerCommand('envsync.bootstrap', () => runTerminalCommand('EnvSync Bootstrap', ['bootstrap'])),
        vscode.commands.registerCommand('envsync.doctor', () => runTerminalCommand('EnvSync Doctor', ['doctor'])),
        vscode.commands.registerCommand('envsync.agentInstall', () => runTerminalCommand('EnvSync Agent Install', ['agent', 'install', '--all'])),
        vscode.commands.registerCommand('envsync.guardScan', () => runTerminalCommand('EnvSync Guard', ['guard', 'scan'])),
        vscode.commands.registerCommand('envsync.run', () => runTerminalCommand('EnvSync Run', ['run'])),
        vscode.commands.registerCommand('envsync.status', () => runTerminalCommand('EnvSync Status', ['status'])),
    );
}

function runTerminalCommand(name: string, args: string[]) {
    const cwd = getWorkspaceFolder();
    if (!cwd) {
        void vscode.window.showWarningMessage('Open a workspace folder before running EnvSync commands.');
        return;
    }

    createEnvSyncTerminal(name, args, cwd);
}
