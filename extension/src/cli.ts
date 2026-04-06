import * as vscode from 'vscode';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { renderCommand } from './shell';

const execFileAsync = promisify(execFile);

export function getWorkspaceFolder(): string | undefined {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

export async function execEnvSync(
    args: string[],
    options: { cwd?: string; timeout?: number } = {},
): Promise<string> {
    const cwd = options.cwd ?? getWorkspaceFolder();
    if (!cwd) {
        throw new Error('No workspace folder is open.');
    }

    try {
        const { stdout, stderr } = await execFileAsync('envsync', args, {
            cwd,
            timeout: options.timeout ?? 15000,
            windowsHide: true,
            maxBuffer: 1024 * 1024,
            encoding: 'utf8',
        });

        return (stdout || stderr).trim();
    } catch (error) {
        const stdout = typeof error === 'object' && error && 'stdout' in error
            ? String((error as { stdout?: string }).stdout || '').trim()
            : '';
        if (typeof error === 'object' && error && 'stderr' in error) {
            const stderr = String((error as { stderr?: string }).stderr || '').trim();
            if (stderr) {
                throw new Error(stdout ? `${stderr}\n${stdout}` : stderr);
            }
        }
        if (stdout) {
            throw new Error(stdout);
        }
        throw error instanceof Error ? error : new Error(String(error));
    }
}

export function createEnvSyncTerminal(name: string, args: string[], cwd: string) {
    const terminal = vscode.window.createTerminal({ name, cwd });
    terminal.show();
    terminal.sendText(renderCommand(args));
}
