import test from 'node:test';
import assert from 'node:assert/strict';
import { assessCliVersion } from '../versionStatus';

test('assessCliVersion treats development builds as ready', () => {
    assert.deepEqual(assessCliVersion('dev', '1.0.0'), {
        kind: 'ready',
        version: 'dev',
    });
});

test('assessCliVersion flags older CLI versions as outdated', () => {
    assert.deepEqual(assessCliVersion('0.9.4', '1.0.0'), {
        kind: 'outdated',
        version: '0.9.4',
        minimumVersion: '1.0.0',
    });
});

test('assessCliVersion accepts matching or newer semantic versions', () => {
    assert.deepEqual(assessCliVersion('1.0.0', '1.0.0'), {
        kind: 'ready',
        version: '1.0.0',
    });
    assert.deepEqual(assessCliVersion('1.2.3', '1.0.0'), {
        kind: 'ready',
        version: '1.2.3',
    });
});
