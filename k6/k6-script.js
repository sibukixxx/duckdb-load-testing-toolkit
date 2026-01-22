import http from 'k6/http';
import { sleep, check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const requestDuration = new Trend('request_duration');

export let options = {
  vus: 100,
  duration: '1m',
  thresholds: {
    errors: ['rate<0.1'],           // Error rate < 10%
    request_duration: ['p(95)<500'], // p95 < 500ms
  },
};

const SIDECAR = __ENV.SIDECAR_BASE || 'http://localhost:8081';
const TARGET = __ENV.TARGET || 'https://httpbin.org/get';
const RUN_ID = __ENV.RUN_ID || 'local-run';
const POD_ID = __ENV.POD_NAME || 'k6-vu';
const BATCH_SIZE = parseInt(__ENV.BATCH_SIZE || '10');

// Event buffer for batch sending
let eventBuffer = [];

export default function () {
  const method = 'GET';
  const startTime = Date.now();

  const res = http.get(TARGET, {
    tags: { name: 'main-request' },
  });

  // Calculate detailed timings
  const timings = res.timings;

  // Build detailed event
  const event = {
    run_id: RUN_ID,
    ts: startTime,
    pod_id: POD_ID,
    vu: __VU,
    iter: __ITER,
    method: method,
    url: TARGET,
    name: 'main-request',
    status: res.status,
    body_len: res.body ? res.body.length : 0,
    rtt: timings.duration,
    dns_lookup: timings.dns || 0,
    tcp_connect: timings.connecting || 0,
    tls_handshake: timings.tls_handshaking || 0,
    ttfb: timings.waiting || 0,
    content_transfer: timings.receiving || 0,
    request_size: res.request.body ? res.request.body.length : 0,
    response_size: res.body ? res.body.length : 0,
  };

  // Add error info if failed
  if (res.status >= 400 || res.error) {
    event.error_code = res.error_code || `HTTP_${res.status}`;
    event.error_msg = res.error || `HTTP error ${res.status}`;
  }

  // Add custom tags
  event.tags = {
    scenario: 'default',
  };

  // Track metrics
  errorRate.add(res.status >= 400);
  requestDuration.add(timings.duration);

  // Check response
  check(res, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
    'response time < 500ms': (r) => r.timings.duration < 500,
  });

  // Add to buffer
  eventBuffer.push(event);

  // Send batch when buffer is full
  if (eventBuffer.length >= BATCH_SIZE) {
    sendBatch();
  }

  sleep(1);
}

// Send events as a batch
function sendBatch() {
  if (eventBuffer.length === 0) return;

  const batch = eventBuffer;
  eventBuffer = [];

  const payload = JSON.stringify({ events: batch });

  http.post(`${SIDECAR}/api/v1/ingest/batch`, payload, {
    headers: { 'Content-Type': 'application/json' },
    timeout: '5000ms',
    tags: { name: 'sidecar-batch' },
  });
}

// Flush remaining events on teardown
export function teardown() {
  sendBatch();
}
