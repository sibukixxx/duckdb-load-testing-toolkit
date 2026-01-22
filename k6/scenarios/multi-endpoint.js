/**
 * Multi-Endpoint Scenario
 * Tests multiple API endpoints with different weights
 */

import { sleep, group } from 'k6';
import { sendRequest, flushEvents, defaultChecks, config } from './base.js';
import { randomItem } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// Configuration
const BASE_URL = __ENV.BASE_URL || 'https://httpbin.org';

// Endpoint definitions with weights
const ENDPOINTS = [
  {
    name: 'get_data',
    method: 'GET',
    path: '/get',
    weight: 50,  // 50% of traffic
    checks: { 'get success': (r) => r.status === 200 },
  },
  {
    name: 'post_data',
    method: 'POST',
    path: '/post',
    weight: 30,  // 30% of traffic
    body: () => JSON.stringify({ timestamp: Date.now(), data: 'test' }),
    headers: { 'Content-Type': 'application/json' },
    checks: { 'post success': (r) => r.status === 200 },
  },
  {
    name: 'put_data',
    method: 'PUT',
    path: '/put',
    weight: 10,  // 10% of traffic
    body: () => JSON.stringify({ updated: true }),
    headers: { 'Content-Type': 'application/json' },
    checks: { 'put success': (r) => r.status === 200 },
  },
  {
    name: 'delete_data',
    method: 'DELETE',
    path: '/delete',
    weight: 10,  // 10% of traffic
    checks: { 'delete success': (r) => r.status === 200 },
  },
];

// Build weighted selection array
const weightedEndpoints = [];
ENDPOINTS.forEach(ep => {
  for (let i = 0; i < ep.weight; i++) {
    weightedEndpoints.push(ep);
  }
});

export let options = {
  scenarios: {
    multi_endpoint: {
      executor: 'constant-arrival-rate',
      rate: 100,              // 100 requests per timeUnit
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    errors: ['rate<0.05'],
    request_duration: ['p(95)<300', 'p(99)<500'],
  },
};

export default function () {
  // Select random endpoint based on weight
  const endpoint = randomItem(weightedEndpoints);

  group(endpoint.name, function () {
    const options = {
      name: endpoint.name,
      checks: endpoint.checks,
      tags: {
        scenario: 'multi_endpoint',
        endpoint: endpoint.name,
      },
    };

    if (endpoint.headers) {
      options.headers = endpoint.headers;
    }

    if (endpoint.body) {
      options.body = typeof endpoint.body === 'function' ? endpoint.body() : endpoint.body;
    }

    sendRequest(endpoint.method, `${BASE_URL}${endpoint.path}`, options);
  });

  // Small random delay to simulate realistic traffic
  sleep(Math.random() * 0.5);
}

export function teardown() {
  flushEvents();
}
