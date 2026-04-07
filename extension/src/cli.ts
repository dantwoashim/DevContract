import * as vscode from 'vscode';
import { execFile } from 'node:child_process';
import { renderCommand } from './shell';

export type ExecCapture = {
    stdout: string;
    stderr: string;
    exitCode: number;
};

export function getWorkspaceFolder(): string | undefined {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

export async function execEnvSync(
    args: string[],
    options: { cwd?: string; timeout?: number } = {},
): Promise<string> {
    const result = await execEnvSyncCapture(args, options);
    if (result.exitCode === 0) {
        return (result.stdout || result.stderr).trim();
    }

    const detail = [result.stderr, result.stdout].filter(Boolean).join('\n').trim();
    if (detail) {
        throw new Error(detail);
    }
    throw new Error(`envsync exited with status ${result.exitCode}`);
}

export async function execEnvSyncCapture(
    args: string[],
    options: { cwd?: string; timeout?: number } = {},
): Promise<ExecCapture> {
    const cwd = options.cwd ?? getWorkspaceFolder();
    if (!cwd) {
        throw new Error('No workspace folder is open.');
    }

    return new Promise<ExecCapture>((resolve, reject) => {
        execFile('envsync', args, {
            cwd,
            timeout: options.timeout ?? 15000,
            windowsHide: true,
            maxBuffer: 1024 * 1024,
            encoding: 'utf8',
        }, (error, stdout, stderr) => {
            const capture = {
                stdout: String(stdout || '').trim(),
                stderr: String(stderr || '').trim(),
                exitCode: 0,
            };

            if (!error) {
                resolve(capture);
                return;
            }

            if (typeof error === 'object' && error && 'code' in error) {
                const code = (error as { code?: number | string }).code;
                if (typeof code === 'number') {
                    resolve({ ...capture, exitCode: code });
                    return;
                }
            }

            reject(error instanceof Error ? error : new Error(String(error)));
        });
    });
}

export function createEnvSyncTerminal(name: string, args: string[], cwd: string) {
    const terminal = vscode.window.createTerminal({ name, cwd });
    terminal.show();
    terminal.sendText(renderCommand(args));
}
