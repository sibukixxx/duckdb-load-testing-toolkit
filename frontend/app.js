import * as duckdb from '@duckdb/duckdb-wasm';
import Chart from 'chart.js/auto';

const bundle = duckdb.getJsDelivrBundles().legacyNode; // fallback bundle selection
const workerUrl = bundle.mainWorker;
const wasmUrl = bundle.mainModule;

const logger = duckdb.ConsoleLogger();

let db;
let charts = {}; // one Chart.js instance per canvas id, torn down before re-render

async function init() {
  const worker = new Worker(workerUrl);
  db = new duckdb.AsyncDuckDB(logger, worker);
  await db.instantiate(wasmUrl);
}
init();

// ---- Tabs -----------------------------------------------------------------

document.getElementById('tabs').addEventListener('click', (ev) => {
  const btn = ev.target.closest('button[data-view]');
  if (!btn) return;

  for (const b of document.querySelectorAll('#tabs button')) {
    b.setAttribute('aria-selected', String(b === btn));
  }
  for (const section of document.querySelectorAll('section.view')) {
    section.classList.toggle('active', section.id === `view-${btn.dataset.view}`);
  }
});

// ---- File loading -----------------------------------------------------------

document.getElementById('loadBtn').onclick = async () => {
  const fileInput = document.getElementById('fileInput');
  if (!fileInput.files || fileInput.files.length === 0) {
    alert('select a .duckdb file');
    return;
  }
  const file = fileInput.files[0];
  const buf = await file.arrayBuffer();
  // Write the file into duckdb-wasm's virtual filesystem, then ATTACH it as
  // a database so its `metrics` table can be queried directly.
  await db.registerFileBuffer(file.name, new Uint8Array(buf));

  if (window.__conn) {
    await window.__conn.close();
  }
  const conn = await db.connect();
  await conn.query(`ATTACH '${file.name}' AS resultdb (READ_ONLY)`);
  await conn.query('USE resultdb');
  window.__conn = conn;

  alert('Loaded. Enter a run_id above and pick a tab to analyze it.');
};

async function query(sql, params = []) {
  const conn = window.__conn || (await db.connect());
  const stmt = await conn.prepare(sql);
  const res = await stmt.query(...params);
  return res.toArray().map((row) => row.toJSON());
}

function renderChart(canvasId, config) {
  if (charts[canvasId]) {
    charts[canvasId].destroy();
  }
  charts[canvasId] = new Chart(document.getElementById(canvasId), config);
}

function renderTable(containerId, columns, rows) {
  const container = document.getElementById(containerId);
  if (rows.length === 0) {
    container.textContent = 'No rows.';
    return;
  }
  const thead = `<tr>${columns.map((c) => `<th>${c.label}</th>`).join('')}</tr>`;
  const tbody = rows
    .map(
      (r) =>
        `<tr>${columns
          .map((c) => `<td>${c.fmt ? c.fmt(r[c.key]) : r[c.key]}</td>`)
          .join('')}</tr>`
    )
    .join('');
  container.innerHTML = `<table><thead>${thead}</thead><tbody>${tbody}</tbody></table>`;
}

const round2 = (v) => (typeof v === 'number' ? Math.round(v * 100) / 100 : v);

// ---- Summary view -----------------------------------------------------------
// Overall run stats plus a status-code distribution chart.

document.getElementById('summaryBtn').onclick = async () => {
  const runId = document.getElementById('summaryRunId').value.trim();
  if (!runId) return alert('enter a run_id');

  const [overall] = await query(
    `SELECT
       COUNT(*) AS request_count,
       COUNT(CASE WHEN status >= 400 OR (error_code IS NOT NULL AND error_code != '') THEN 1 END) AS error_count,
       AVG(rtt) AS avg_rtt,
       PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) AS p50_rtt,
       PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) AS p95_rtt,
       PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) AS p99_rtt
     FROM metrics WHERE run_id = ?`,
    [runId]
  );

  document.getElementById('summaryOut').textContent = JSON.stringify(
    overall ? Object.fromEntries(Object.entries(overall).map(([k, v]) => [k, round2(v)])) : {},
    null,
    2
  );

  const statusRows = await query(
    `SELECT status, COUNT(*) AS cnt FROM metrics WHERE run_id = ? GROUP BY status ORDER BY status`,
    [runId]
  );
  renderChart('summaryChart', {
    type: 'bar',
    data: {
      labels: statusRows.map((r) => r.status),
      datasets: [{ label: 'requests', data: statusRows.map((r) => r.cnt) }],
    },
  });
};

// ---- Endpoints view ----------------------------------------------------------
// Per-endpoint request/error counts, latency percentiles, status distribution.

document.getElementById('endpointsBtn').onclick = async () => {
  const runId = document.getElementById('endpointsRunId').value.trim();
  if (!runId) return alert('enter a run_id');

  const rows = await query(
    `SELECT
       url AS endpoint,
       COUNT(*) AS request_count,
       COUNT(CASE WHEN status >= 400 OR (error_code IS NOT NULL AND error_code != '') THEN 1 END) AS error_count,
       AVG(rtt) AS avg_rtt,
       PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) AS p50_rtt,
       PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) AS p95_rtt,
       PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) AS p99_rtt
     FROM metrics WHERE run_id = ?
     GROUP BY url ORDER BY request_count DESC`,
    [runId]
  );

  renderTable(
    'endpointsTable',
    [
      { key: 'endpoint', label: 'Endpoint' },
      { key: 'request_count', label: 'Requests' },
      { key: 'error_count', label: 'Errors' },
      { key: 'avg_rtt', label: 'Avg (ms)', fmt: round2 },
      { key: 'p50_rtt', label: 'P50 (ms)', fmt: round2 },
      { key: 'p95_rtt', label: 'P95 (ms)', fmt: round2 },
      { key: 'p99_rtt', label: 'P99 (ms)', fmt: round2 },
    ],
    rows
  );
};

// ---- Regression view ----------------------------------------------------------
// Per-endpoint baseline vs. current p95 comparison (absolute + percentage delta).

document.getElementById('regressionBtn').onclick = async () => {
  const baselineId = document.getElementById('regBaselineId').value.trim();
  const currentId = document.getElementById('regCurrentId').value.trim();
  if (!baselineId || !currentId) return alert('enter both run ids');

  const perRun = async (runId) =>
    query(
      `SELECT
         url AS endpoint,
         COUNT(*) AS request_count,
         COUNT(CASE WHEN status >= 400 OR (error_code IS NOT NULL AND error_code != '') THEN 1 END) AS error_count,
         PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) AS p95_rtt
       FROM metrics WHERE run_id = ?
       GROUP BY url`,
      [runId]
    );

  const [baseline, current] = await Promise.all([perRun(baselineId), perRun(currentId)]);
  const baselineByEndpoint = Object.fromEntries(baseline.map((r) => [r.endpoint, r]));

  const rows = current
    .filter((c) => baselineByEndpoint[c.endpoint])
    .map((c) => {
      const b = baselineByEndpoint[c.endpoint];
      const p95Delta = c.p95_rtt - b.p95_rtt;
      const p95ChangePct = b.p95_rtt ? (p95Delta / b.p95_rtt) * 100 : 0;
      const errRateBaseline = b.request_count ? (b.error_count / b.request_count) * 100 : 0;
      const errRateCurrent = c.request_count ? (c.error_count / c.request_count) * 100 : 0;
      return {
        endpoint: c.endpoint,
        baseline_p95: b.p95_rtt,
        current_p95: c.p95_rtt,
        p95_delta: p95Delta,
        p95_change_pct: p95ChangePct,
        error_rate_delta: errRateCurrent - errRateBaseline,
      };
    })
    .sort((a, b) => b.p95_change_pct - a.p95_change_pct);

  renderTable(
    'regressionTable',
    [
      { key: 'endpoint', label: 'Endpoint' },
      { key: 'baseline_p95', label: 'Baseline P95 (ms)', fmt: round2 },
      { key: 'current_p95', label: 'Current P95 (ms)', fmt: round2 },
      { key: 'p95_delta', label: 'Delta (ms)', fmt: round2 },
      { key: 'p95_change_pct', label: 'Change (%)', fmt: round2 },
      { key: 'error_rate_delta', label: 'Error Rate Delta (pp)', fmt: round2 },
    ],
    rows
  );
};

// ---- Timing breakdown view ----------------------------------------------------
// DNS/TCP/TLS/TTFB/transfer averages for one endpoint, baseline vs. current.

document.getElementById('timingBtn').onclick = async () => {
  const baselineId = document.getElementById('timingBaselineId').value.trim();
  const currentId = document.getElementById('timingCurrentId').value.trim();
  const endpoint = document.getElementById('timingEndpoint').value.trim();
  if (!baselineId || !currentId || !endpoint) return alert('enter both run ids and an endpoint');

  const breakdown = async (runId) => {
    const [row] = await query(
      `SELECT
         AVG(dns_lookup) AS dns,
         AVG(tcp_connect) AS tcp,
         AVG(tls_handshake) AS tls,
         AVG(ttfb) AS ttfb,
         AVG(content_transfer) AS transfer
       FROM metrics WHERE run_id = ? AND url = ?`,
      [runId, endpoint]
    );
    return row || { dns: 0, tcp: 0, tls: 0, ttfb: 0, transfer: 0 };
  };

  const [baseline, current] = await Promise.all([breakdown(baselineId), breakdown(currentId)]);
  const phases = ['dns', 'tcp', 'tls', 'ttfb', 'transfer'];

  renderChart('timingChart', {
    type: 'bar',
    data: {
      labels: phases.map((p) => p.toUpperCase()),
      datasets: [
        { label: 'baseline', data: phases.map((p) => round2(baseline[p])) },
        { label: 'current', data: phases.map((p) => round2(current[p])) },
      ],
    },
  });
};
