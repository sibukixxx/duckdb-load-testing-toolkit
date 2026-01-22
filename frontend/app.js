import * as duckdb from '@duckdb/duckdb-wasm';
import Chart from 'chart.js/auto';

const bundle = duckdb.getJsDelivrBundles().legacyNode; // fallback bundle selection
const workerUrl = bundle.mainWorker;
const wasmUrl = bundle.mainModule;

const logger = duckdb.ConsoleLogger();

let db;

async function init() {
  const worker = new Worker(workerUrl);
  const workerss = new duckdb.AsyncDuckDB(logger, worker);
  db = await workerss.open();
}
init();

document.getElementById('loadBtn').onclick = async () => {
  const fileInput = document.getElementById('fileInput');
  if (!fileInput.files || fileInput.files.length === 0) {
    alert('select a .duckdb file');
    return;
  }
  const file = fileInput.files[0];
  const buf = await file.arrayBuffer();
  // write file into the virtual filesystem
  await db.registerFile(file.name, new Uint8Array(buf));
  const conn = await db.connect();
  const res = await conn.query('SELECT status, COUNT(*) AS cnt, AVG(rtt) AS avg_rtt FROM metrics GROUP BY status ORDER BY status');
  document.getElementById('out').textContent = JSON.stringify(res, null, 2);
  const labels = res.map(r => r.status);
  const data = res.map(r => r.cnt);
  new Chart(document.getElementById('chart'), {
    type: 'bar',
    data: { labels, datasets: [{ label: 'count', data }] },
  });
};
