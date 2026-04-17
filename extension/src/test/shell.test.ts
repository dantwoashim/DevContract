import test from 'node:test';
import assert from 'node:assert/strict';
import { quoteArg, renderCommand } from '../shell';

test('quoteArg leaves simple values untouched', () => {
    assert.equal(quoteArg('doctor'), 'doctor');
    assert.equal(quoteArg('./path/to/file.env'), './path/to/file.env');
});

test('quoteArg escapes whitespace and quotes', () => {
    assert.equal(quoteArg('my file.env'), '"my file.env"');
    assert.equal(quoteArg('say "hi"'), '"say \\"hi\\""');
});

test('renderCommand prefixes devcontract and quotes arguments as needed', () => {
    assert.equal(
        renderCommand(['pull', '--file', 'my file.env']),
        'devcontract pull --file "my file.env"',
    );
});
