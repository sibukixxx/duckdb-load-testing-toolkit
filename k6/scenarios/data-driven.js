/**
 * Data-Driven Scenario
 * Reads test data from shared array and iterates through it
 */

import { sleep, group } from 'k6';
import { SharedArray } from 'k6/data';
import { sendRequest, flushEvents, defaultChecks, config } from './base.js';

// Configuration
const BASE_URL = __ENV.BASE_URL || 'https://httpbin.org';

// Load test data (could be from CSV, JSON file, etc.)
// In production, use: new SharedArray('users', function () { return JSON.parse(open('./data/users.json')); });
const testData = new SharedArray('testdata', function () {
  // Simulated test data - in production load from file
  return [
    { userId: 'user001', action: 'view', resource: 'dashboard' },
    { userId: 'user002', action: 'edit', resource: 'profile' },
    { userId: 'user003', action: 'create', resource: 'post' },
    { userId: 'user004', action: 'delete', resource: 'comment' },
    { userId: 'user005', action: 'view', resource: 'settings' },
    { userId: 'user006', action: 'update', resource: 'preferences' },
    { userId: 'user007', action: 'view', resource: 'notifications' },
    { userId: 'user008', action: 'create', resource: 'message' },
    { userId: 'user009', action: 'view', resource: 'analytics' },
    { userId: 'user010', action: 'export', resource: 'report' },
  ];
});

export let options = {
  scenarios: {
    data_driven: {
      executor: 'per-vu-iterations',
      vus: 10,
      iterations: 10,  // Each VU runs through 10 data items
      maxDuration: '5m',
    },
  },
  thresholds: {
    errors: ['rate<0.1'],
    request_duration: ['p(95)<500'],
  },
};

export default function () {
  // Get data item for this iteration
  const dataIndex = __ITER % testData.length;
  const data = testData[dataIndex];

  group(`user_${data.userId}`, function () {
    // Map action to HTTP method
    const methodMap = {
      'view': 'GET',
      'create': 'POST',
      'edit': 'PUT',
      'update': 'PATCH',
      'delete': 'DELETE',
      'export': 'GET',
    };

    const method = methodMap[data.action] || 'GET';
    const path = `/anything/${data.resource}`;

    const options = {
      name: `${data.action}_${data.resource}`,
      checks: defaultChecks({ statusCode: 200 }),
      tags: {
        scenario: 'data_driven',
        user_id: data.userId,
        action: data.action,
        resource: data.resource,
      },
    };

    // Add body for write operations
    if (['POST', 'PUT', 'PATCH'].includes(method)) {
      options.body = JSON.stringify({
        userId: data.userId,
        action: data.action,
        resource: data.resource,
        timestamp: Date.now(),
      });
      options.headers = { 'Content-Type': 'application/json' };
    }

    sendRequest(method, `${BASE_URL}${path}`, options);
  });

  sleep(0.5);
}

export function teardown() {
  flushEvents();
}
