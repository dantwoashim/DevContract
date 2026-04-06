export function renderCommand(args: string[]): string {
    return ['envsync', ...args.map(quoteArg)].join(' ');
}

export function quoteArg(value: string): string {
    if (!/[^\w./:-]/.test(value)) {
        return value;
    }
    return `"${value.replace(/"/g, '\\"')}"`;
}
