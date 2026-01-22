/**
 * Authentication Flow Scenario
 * Demonstrates login -> authenticated API calls -> logout flow
 */

import { sleep, group } from 'k6';
import { sendRequest, flushEvents, defaultChecks, parseJSON, config } from './base.js';

// Configuration
const BASE_URL = __ENV.BASE_URL || 'https://httpbin.org';
const USERNAME = __ENV.USERNAME || 'testuser';
const PASSWORD = __ENV.PASSWORD || 'testpass';

export let options = {
  scenarios: {
    auth_flow: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '1m', target: 10 },
        { duration: '30s', target: 0 },
      ],
    },
  },
  thresholds: {
    errors: ['rate<0.1'],
    request_duration: ['p(95)<1000'],
  },
};

export default function () {
  let authToken = null;

  // Step 1: Login
  group('login', function () {
    const loginPayload = JSON.stringify({
      username: USERNAME,
      password: PASSWORD,
    });

    const res = sendRequest('POST', `${BASE_URL}/post`, {
      name: 'login',
      body: loginPayload,
      headers: { 'Content-Type': 'application/json' },
      checks: {
        'login successful': (r) => r.status === 200,
      },
      tags: {
        scenario: 'auth_flow',
        step: 'login',
      },
    });

    // Extract token from response (simulated)
    const body = parseJSON(res);
    if (body && body.json) {
      // In real scenario, extract actual token
      authToken = `Bearer ${body.json.username}_token_${Date.now()}`;
    }
  });

  sleep(0.5);

  // Step 2: Authenticated API calls
  if (authToken) {
    group('authenticated_requests', function () {
      // Get user profile
      sendRequest('GET', `${BASE_URL}/get`, {
        name: 'get_profile',
        headers: {
          'Authorization': authToken,
        },
        checks: defaultChecks({ statusCode: 200, maxDuration: 500 }),
        tags: {
          scenario: 'auth_flow',
          step: 'get_profile',
        },
      });

      sleep(0.3);

      // Update settings
      sendRequest('PUT', `${BASE_URL}/put`, {
        name: 'update_settings',
        body: JSON.stringify({ theme: 'dark', notifications: true }),
        headers: {
          'Authorization': authToken,
          'Content-Type': 'application/json',
        },
        checks: defaultChecks({ statusCode: 200 }),
        tags: {
          scenario: 'auth_flow',
          step: 'update_settings',
        },
      });

      sleep(0.3);

      // Perform action
      sendRequest('POST', `${BASE_URL}/post`, {
        name: 'perform_action',
        body: JSON.stringify({ action: 'submit', data: { value: 42 } }),
        headers: {
          'Authorization': authToken,
          'Content-Type': 'application/json',
        },
        checks: defaultChecks({ statusCode: 200 }),
        tags: {
          scenario: 'auth_flow',
          step: 'perform_action',
        },
      });
    });
  }

  sleep(0.5);

  // Step 3: Logout
  group('logout', function () {
    sendRequest('POST', `${BASE_URL}/post`, {
      name: 'logout',
      body: JSON.stringify({ action: 'logout' }),
      headers: {
        'Authorization': authToken,
        'Content-Type': 'application/json',
      },
      checks: {
        'logout successful': (r) => r.status === 200,
      },
      tags: {
        scenario: 'auth_flow',
        step: 'logout',
      },
    });
  });

  sleep(1);
}

export function teardown() {
  flushEvents();
}
