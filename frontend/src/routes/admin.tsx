import { useEffect, useRef, useState } from 'preact/hooks';
import { trackPageView } from '../lib/tracking';

type StatusRow = {
  status: number | string;
  cnt: number;
  avg_rtt: number;
};

type DuckDBHandle = {
  db: import('@duckdb/duckdb-wasm').AsyncDuckDB;
  worker: Worker;
};

async function createDuckDB(): Promise<DuckDBHandle> {
  const duckdb = await import('@duckdb/duckdb-wasm');
  const bundles = duckdb.getJsDelivrBundles();
  const bundle = await duckdb.selectBundle(bundles);
  const workerUrl = URL.createObjectURL(
    new Blob([`importScripts("${bundle.mainWorker!}");`], { type: 'text/javascript' }),
  );
  const worker = new Worker(workerUrl);
  const logger = new duckdb.ConsoleLogger();
  const db = new duckdb.AsyncDuckDB(logger, worker);
  await db.instantiate(bundle.mainModule, bundle.pthreadWorker);
  URL.revokeObjectURL(workerUrl);
  return { db, worker };
}

export function Admin() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<unknown>(null);
  const handleRef = useRef<DuckDBHandle | null>(null);
  const [status, setStatus] = useState<'idle' | 'loading-db' | 'ready' | 'querying' | 'error'>(
    'idle',
  );
  const [rows, setRows] = useState<StatusRow[]>([]);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    trackPageView('/admin');
    if (typeof window === 'undefined') {
      return;
    }
    let cancelled = false;
    setStatus('loading-db');
    createDuckDB()
      .then((handle) => {
        if (cancelled) {
          handle.worker.terminate();
          return;
        }
        handleRef.current = handle;
        setStatus('ready');
      })
      .catch((err: unknown) => {
        setStatus('error');
        setErrorMsg(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
      handleRef.current?.worker.terminate();
      handleRef.current = null;
    };
  }, []);

  const handleLoad = async (event: Event) => {
    event.preventDefault();
    const input = (event.currentTarget as HTMLFormElement).elements.namedItem(
      'duckdbFile',
    ) as HTMLInputElement | null;
    const file = input?.files?.[0];
    if (!file) {
      alert('select a .duckdb file');
      return;
    }
    const handle = handleRef.current;
    if (!handle) {
      alert('DuckDB is not ready yet');
      return;
    }
    setStatus('querying');
    setErrorMsg(null);
    try {
      const buf = await file.arrayBuffer();
      await handle.db.registerFileBuffer(file.name, new Uint8Array(buf));
      const conn = await handle.db.connect();
      try {
        await conn.query(`ATTACH '${file.name}' AS loaded (READ_ONLY)`);
        const res = await conn.query(
          'SELECT status, COUNT(*) AS cnt, AVG(rtt) AS avg_rtt FROM loaded.metrics GROUP BY status ORDER BY status',
        );
        const parsed: StatusRow[] = res.toArray().map((row: Record<string, unknown>) => ({
          status: row.status as number,
          cnt: Number(row.cnt),
          avg_rtt: Number(row.avg_rtt),
        }));
        setRows(parsed);
        await renderChart(canvasRef.current, chartRef, parsed);
      } finally {
        await conn.close();
      }
      setStatus('ready');
    } catch (err) {
      setStatus('error');
      setErrorMsg(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <section>
      <div class="card">
        <h2>Admin — DuckDB Viewer</h2>
        <p>
          k6 + sidecar が出力した <code>.duckdb</code> ファイルをブラウザで読み込み、
          status 別の件数と平均 RTT を表示します。
        </p>
        <p>Status: {status}</p>
        {errorMsg ? <p style={{ color: '#b91c1c' }}>{errorMsg}</p> : null}
        <form onSubmit={handleLoad}>
          <input id="duckdbFile" name="duckdbFile" type="file" accept=".duckdb" />
          <button type="submit" disabled={status !== 'ready' && status !== 'querying'}>
            Load file
          </button>
        </form>
      </div>
      <div class="card">
        <h3>結果</h3>
        <pre>{JSON.stringify(rows, null, 2)}</pre>
        <canvas ref={canvasRef} width={600} height={300} />
      </div>
    </section>
  );
}

async function renderChart(
  canvas: HTMLCanvasElement | null,
  chartRef: { current: unknown },
  rows: StatusRow[],
): Promise<void> {
  if (!canvas) {
    return;
  }
  const { default: Chart } = await import('chart.js/auto');
  const existing = chartRef.current as { destroy?: () => void } | null;
  existing?.destroy?.();
  chartRef.current = new Chart(canvas, {
    type: 'bar',
    data: {
      labels: rows.map((r) => String(r.status)),
      datasets: [{ label: 'count', data: rows.map((r) => r.cnt) }],
    },
  });
}
