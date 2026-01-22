/**
 * Base module for k6 scenarios
 * Provides common utilities for metrics collection
 */

import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Shared metrics
export const errorRate = new Rate('errors');
export const requestDuration = new Trend('request_duration');
export const requestCount = new Counter('requests');

// Configuration
export const config = {
  sidecar: __ENV.SIDECAR_BASE || 'http://localhost:8081',
  runId: __ENV.RUN_ID || 'local-run',
  podId: __ENV.POD_NAME || 'k6-vu',
  batchSize: parseInt(__ENV.BATCH_SIZE || '10'),
};

// Event buffer
let eventBuffer = [];

/**
 * Build a detailed event from an HTTP response
 * @param {Object} res - k6 HTTP response
 * @param {Object} options - Additional options
 * @returns {Object} Event object
 */
export function buildEvent(res, options = {}) {
  const timings = res.timings;

  const event = {
    run_id: config.runId,
    ts: Date.now(),
    pod_id: config.podId,
    vu: __VU,
    iter: __ITER,
    method: options.method || 'GET',
    url: res.url,
    name: options.name || 'request',
    status: res.status,
    body_len: res.body ? res.body.length : 0,
    rtt: timings.duration,
    dns_lookup: timings.dns || 0,
    tcp_connect: timings.connecting || 0,
    tls_handshake: timings.tls_handshaking || 0,
    ttfb: timings.waiting || 0,
    content_transfer: timings.receiving || 0,
    request_size: options.requestSize || 0,
    response_size: res.body ? res.body.length : 0,
    tags: options.tags || {},
  };

  // Add error info if failed
  if (res.status >= 400 || res.error) {
    event.error_code = res.error_code || `HTTP_${res.status}`;
    event.error_msg = res.error || `HTTP error ${res.status}`;
  }

  return event;
}

/**
 * Send a request and record metrics
 * @param {string} method - HTTP method
 * @param {string} url - Request URL
 * @param {Object} options - Request options
 * @returns {Object} HTTP response
 */
export function sendRequest(method, url, options = {}) {
  const params = {
    headers: options.headers || {},
    timeout: options.timeout || '30s',
    tags: { name: options.name || 'request' },
  };

  let res;
  let requestSize = 0;

  switch (method.toUpperCase()) {
    case 'GET':
      res = http.get(url, params);
      break;
    case 'POST':
      requestSize = options.body ? options.body.length : 0;
      res = http.post(url, options.body, params);
      break;
    case 'PUT':
      requestSize = options.body ? options.body.length : 0;
      res = http.put(url, options.body, params);
      break;
    case 'DELETE':
      res = http.del(url, options.body, params);
      break;
    case 'PATCH':
      requestSize = options.body ? options.body.length : 0;
      res = http.patch(url, options.body, params);
      break;
    default:
      throw new Error(`Unsupported method: ${method}`);
  }

  // Record metrics
  errorRate.add(res.status >= 400);
  requestDuration.add(res.timings.duration);
  requestCount.add(1);

  // Run checks if provided
  if (options.checks) {
    check(res, options.checks);
  }

  // Build and buffer event
  const event = buildEvent(res, {
    method,
    name: options.name,
    requestSize,
    tags: options.tags,
  });
  bufferEvent(event);

  return res;
}

/**
 * Add event to buffer
 * @param {Object} event - Event to buffer
 */
export function bufferEvent(event) {
  eventBuffer.push(event);

  if (eventBuffer.length >= config.batchSize) {
    flushEvents();
  }
}

/**
 * Flush buffered events to sidecar
 */
export function flushEvents() {
  if (eventBuffer.length === 0) return;

  const batch = eventBuffer;
  eventBuffer = [];

  const payload = JSON.stringify({ events: batch });

  http.post(`${config.sidecar}/api/v1/ingest/batch`, payload, {
    headers: { 'Content-Type': 'application/json' },
    timeout: '5000ms',
    tags: { name: 'sidecar-batch' },
  });
}

/**
 * Parse JSON response safely
 * @param {Object} res - HTTP response
 * @returns {Object|null} Parsed JSON or null
 */
export function parseJSON(res) {
  try {
    return JSON.parse(res.body);
  } catch (e) {
    return null;
  }
}

/**
 * Create default checks for HTTP responses
 * @param {Object} options - Check options
 * @returns {Object} Check functions
 */
export function defaultChecks(options = {}) {
  const statusCode = options.statusCode || 200;
  const maxDuration = options.maxDuration || 500;

  return {
    [`status is ${statusCode}`]: (r) => r.status === statusCode,
    [`response time < ${maxDuration}ms`]: (r) => r.timings.duration < maxDuration,
  };
}
