import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Counter } from 'k6/metrics';

const errorRate = new Rate('errors');
const blobsUploaded = new Counter('blobs_uploaded');
const blobsDownloaded = new Counter('blobs_downloaded');

export const options = {
    stages: [
        { duration: '10s', target: 10 },   // Ramp up
        { duration: '30s', target: 50 },   // Sustained load
        { duration: '10s', target: 100 },  // Peak
        { duration: '10s', target: 0 },    // Ramp down
    ],
    thresholds: {
        http_req_duration: ['p(95)<500'],  // 95th percentile < 500ms
        errors: ['rate<0.1'],              // Error rate < 10%
    },
};

const BASE_URL = __ENV.RELAY_URL || 'https://relay.devcontract.dev';

export default function () {
    const teamId = `load-test-team-${__VU}`;
    const blobId = `blob-${__VU}-${__ITER}`;

    // 1. Health check
    const healthRes = http.get(`${BASE_URL}/health`);
    check(healthRes, {
        'health: status 200': (r) => r.status === 200,
        'health: has status field': (r) => JSON.parse(r.body).status === 'ok',
    });

    // 2. Upload blob
    const blobData = generateRandomBytes(1024); // 1KB test blob
    const uploadRes = http.put(
        `${BASE_URL}/relay/${teamId}/${blobId}`,
        blobData,
        {
            headers: {
                'Content-Type': 'application/octet-stream',
                'X-DevContract-Sender': `SHA256:sender-${__VU}`,
                'X-DevContract-Recipient': `SHA256:recipient-${__VU}`,
                'X-DevContract-EphemeralKey': 'dGVzdGtleQ==',
                'X-DevContract-Filename': '.env',
            },
        },
    );

    const uploadOk = check(uploadRes, {
        'upload: status 201 or 429': (r) => r.status === 201 || r.status === 429,
    });
    if (uploadOk && uploadRes.status === 201) {
        blobsUploaded.add(1);
    }
    errorRate.add(!uploadOk);

    sleep(0.1);

    // 3. List pending
    const pendingRes = http.get(
        `${BASE_URL}/relay/${teamId}/pending?for=SHA256:recipient-${__VU}`,
    );
    check(pendingRes, {
        'pending: status 200': (r) => r.status === 200,
    });

    // 4. Download blob
    const downloadRes = http.get(`${BASE_URL}/relay/${teamId}/${blobId}`);
    const downloadOk = check(downloadRes, {
        'download: status 200 or 404': (r) => r.status === 200 || r.status === 404,
    });
    if (downloadOk && downloadRes.status === 200) {
        blobsDownloaded.add(1);
    }

    sleep(0.1);

    // 5. Delete blob
    http.del(`${BASE_URL}/relay/${teamId}/${blobId}`, null, {
        headers: { 'X-DevContract-Fingerprint': `SHA256:recipient-${__VU}` },
    });

    // 6. Team member CRUD
    const memberRes = http.put(
        `${BASE_URL}/teams/${teamId}/members/loadtest-user-${__VU}`,
        JSON.stringify({
            fingerprint: `SHA256:fp-${__VU}`,
            public_key: 'dGVzdHB1YmtleQ==',
        }),
        { headers: { 'Content-Type': 'application/json' } },
    );
    check(memberRes, {
        'member: status 200 or 429': (r) => r.status === 200 || r.status === 429,
    });

    sleep(0.5);
}

function generateRandomBytes(size) {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    let result = '';
    for (let i = 0; i < size; i++) {
        result += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    return result;
}
