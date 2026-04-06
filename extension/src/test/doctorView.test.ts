import test from 'node:test';
import assert from 'node:assert/strict';
import { buildHealthRows, iconForStatus, type DoctorReport } from '../doctorView';

test('buildHealthRows includes summary rows and every doctor check', () => {
    const report: DoctorReport = {
        blocking: 1,
        warnings: 2,
        checks: [
            { name: 'config', status: 'pass', detail: 'loaded' },
            { name: 'identity', status: 'warn', detail: 'missing key' },
            { name: 'relay', status: 'fail', detail: 'offline' },
        ],
    };

    const rows = buildHealthRows(report);
    assert.equal(rows.length, 5);
    assert.deepEqual(rows[0], { label: 'Blocking Issues', description: '1' });
    assert.deepEqual(rows[1], { label: 'Warnings', description: '2' });
    assert.deepEqual(rows[4], { label: '$(error) relay', description: 'offline' });
});

test('iconForStatus maps known statuses and defaults unknown to error', () => {
    assert.equal(iconForStatus('pass'), '$(check)');
    assert.equal(iconForStatus('warn'), '$(warning)');
    assert.equal(iconForStatus('anything-else'), '$(error)');
});
