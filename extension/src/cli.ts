import * as vscode from 'vscode';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

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
        if (typeof error === 'object' && error && 'stdout' in error) {
            const stdout = String((error as { stdout?: string }).stdout || '').trim();
            if (stdout) {
                return stdout;
            }
        }
        if (typeof error === 'object' && error && 'stderr' in error) {
            const stderr = String((error as { stderr?: string }).stderr || '').trim();
            if (stderr) {
                throw new Error(stderr);
            }
        }
        throw error instanceof Error ? error : new Error(String(error));
    }
}

export function createEnvSyncTerminal(name: string, args: string[], cwd: string) {
    const terminal = vscode.window.createTerminal({ name, cwd });
    terminal.show();
    terminal.sendText(renderCommand(args));
}

function renderCommand(args: string[]): string {
    return ['envsync', ...args.map(quoteArg)].join(' ');
}

function quoteArg(value: string): string {
    if (!/[^\w./:-]/.test(value)) {
        return value;
    }
    return `"${value.replace(/"/g, '\\"')}"`;
}
