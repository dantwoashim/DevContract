#!/usr/bin/env node

const fs = require('node:fs');
const path = require('node:path');

const REPO_ROOT = path.resolve(__dirname, '..');
const SERVER_NAME = 'envsync-repo-docs';
const SERVER_VERSION = '1.0.0';

const DOCS = [
    {
        uri: 'repo://README.md',
        path: 'README.md',
        name: 'README',
        description: 'Public project overview and quick start',
        mimeType: 'text/markdown',
    },
    {
        uri: 'repo://docs/CONTRACT.md',
        path: 'docs/CONTRACT.md',
        name: 'Contract Reference',
        description: 'Repository contract schema and usage',
        mimeType: 'text/markdown',
    },
    {
        uri: 'repo://docs/OPERATIONS.md',
        path: 'docs/OPERATIONS.md',
        name: 'Operations Guide',
        description: 'Relay deployment, recovery, and operational guidance',
        mimeType: 'text/markdown',
    },
    {
        uri: 'repo://docs/RELEASES.md',
        path: 'docs/RELEASES.md',
        name: 'Release Guide',
        description: 'Release packaging and verification notes',
        mimeType: 'text/markdown',
    },
    {
        uri: 'repo://SECURITY.md',
        path: 'SECURITY.md',
        name: 'Security Policy',
        description: 'Security model and disclosure process',
        mimeType: 'text/markdown',
    },
    {
        uri: 'repo://CONTRIBUTING.md',
        path: 'CONTRIBUTING.md',
        name: 'Contributing',
        description: 'Development workflow and contribution guide',
        mimeType: 'text/markdown',
    },
];

if (process.argv.includes('--self-check')) {
    const missing = DOCS
        .map((doc) => ({ ...doc, absolute: path.join(REPO_ROOT, doc.path) }))
        .filter((doc) => !fs.existsSync(doc.absolute))
        .map((doc) => doc.path);

    if (missing.length > 0) {
        console.error(`missing repo docs: ${missing.join(', ')}`);
        process.exit(1);
    }

    process.stdout.write(JSON.stringify({
        status: 'ok',
        server: SERVER_NAME,
        docs: DOCS.map((doc) => doc.path),
    }, null, 2));
    process.exit(0);
}

let input = Buffer.alloc(0);
process.stdin.on('data', (chunk) => {
    input = Buffer.concat([input, chunk]);
    processBuffer();
});

process.stdin.on('end', () => process.exit(0));

function processBuffer() {
    for (;;) {
        const headerEnd = input.indexOf('\r\n\r\n');
        if (headerEnd < 0) {
            return;
        }

        const headerText = input.slice(0, headerEnd).toString('utf8');
        const lengthMatch = /Content-Length:\s*(\d+)/i.exec(headerText);
        if (!lengthMatch) {
            input = Buffer.alloc(0);
            return;
        }

        const bodyLength = Number(lengthMatch[1]);
        const bodyStart = headerEnd + 4;
        if (input.length < bodyStart + bodyLength) {
            return;
        }

        const body = input.slice(bodyStart, bodyStart + bodyLength).toString('utf8');
        input = input.slice(bodyStart + bodyLength);

        let message;
        try {
            message = JSON.parse(body);
        } catch {
            continue;
        }

        void handleMessage(message);
    }
}

async function handleMessage(message) {
    if (!message || typeof message !== 'object' || !('id' in message)) {
        return;
    }

    try {
        const result = await dispatch(message.method, message.params || {});
        send({
            jsonrpc: '2.0',
            id: message.id,
            result,
        });
    } catch (error) {
        send({
            jsonrpc: '2.0',
            id: message.id,
            error: {
                code: -32000,
                message: error instanceof Error ? error.message : String(error),
            },
        });
    }
}

async function dispatch(method, params) {
    switch (method) {
        case 'initialize':
            return {
                protocolVersion: '2025-03-26',
                capabilities: {
                    tools: {},
                    resources: {
                        subscribe: false,
                        listChanged: false,
                    },
                },
                serverInfo: {
                    name: SERVER_NAME,
                    version: SERVER_VERSION,
                },
            };
        case 'ping':
            return {};
        case 'tools/list':
            return {
                tools: [
                    {
                        name: 'list_repo_docs',
                        description: 'List the EnvSync repository documents exposed by this MCP server.',
                        inputSchema: {
                            type: 'object',
                            properties: {},
                            additionalProperties: false,
                        },
                    },
                    {
                        name: 'read_repo_doc',
                        description: 'Read a repository document by relative path.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                path: {
                                    type: 'string',
                                    description: 'Relative path such as README.md or docs/CONTRACT.md',
                                },
                            },
                            required: ['path'],
                            additionalProperties: false,
                        },
                    },
                    {
                        name: 'search_repo_docs',
                        description: 'Search the exposed repository documents for a text query.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                query: {
                                    type: 'string',
                                    description: 'Case-insensitive search string',
                                },
                            },
                            required: ['query'],
                            additionalProperties: false,
                        },
                    },
                ],
            };
        case 'tools/call':
            return callTool(params);
        case 'resources/list':
            return {
                resources: DOCS.map((doc) => ({
                    uri: doc.uri,
                    name: doc.name,
                    description: doc.description,
                    mimeType: doc.mimeType,
                })),
            };
        case 'resources/read':
            return readResource(params.uri);
        default:
            throw new Error(`unsupported method: ${method}`);
    }
}

function callTool(params) {
    const name = params && params.name;
    const args = params && typeof params.arguments === 'object' && params.arguments ? params.arguments : {};

    switch (name) {
        case 'list_repo_docs':
            return {
                content: [
                    {
                        type: 'text',
                        text: DOCS.map((doc) => `${doc.path} - ${doc.description}`).join('\n'),
                    },
                ],
            };
        case 'read_repo_doc':
            return {
                content: [
                    {
                        type: 'text',
                        text: readDoc(String(args.path || '').trim()),
                    },
                ],
            };
        case 'search_repo_docs': {
            const query = String(args.query || '').trim().toLowerCase();
            if (!query) {
                throw new Error('query is required');
            }

            const matches = [];
            for (const doc of DOCS) {
                const lines = readDoc(doc.path).split(/\r?\n/);
                for (let index = 0; index < lines.length; index += 1) {
                    if (lines[index].toLowerCase().includes(query)) {
                        matches.push(`${doc.path}:${index + 1}: ${lines[index].trim()}`);
                    }
                }
            }

            return {
                content: [
                    {
                        type: 'text',
                        text: matches.length > 0 ? matches.join('\n') : 'No matches found.',
                    },
                ],
            };
        }
        default:
            throw new Error(`unsupported tool: ${name}`);
    }
}

function readResource(uri) {
    const doc = DOCS.find((candidate) => candidate.uri === uri);
    if (!doc) {
        throw new Error(`unknown resource: ${uri}`);
    }

    return {
        contents: [
            {
                uri: doc.uri,
                mimeType: doc.mimeType,
                text: readDoc(doc.path),
            },
        ],
    };
}

function readDoc(repoPath) {
    const doc = DOCS.find((candidate) => candidate.path === repoPath);
    if (!doc) {
        throw new Error(`unsupported doc path: ${repoPath}`);
    }

    return fs.readFileSync(path.join(REPO_ROOT, doc.path), 'utf8');
}

function send(payload) {
    const body = JSON.stringify(payload);
    const headers = `Content-Length: ${Buffer.byteLength(body, 'utf8')}\r\nContent-Type: application/json\r\n\r\n`;
    process.stdout.write(headers + body);
}
