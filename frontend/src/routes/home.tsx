import { useEffect } from 'preact/hooks';
import { trackPageView } from '../lib/tracking';

export function Home() {
  useEffect(() => {
    trackPageView('/');
  }, []);

  return (
    <section>
      <div class="card">
        <h2>DuckDB Load Testing Frontend</h2>
        <p>
          k6 + sidecar-go + DuckDB の負荷テスト結果をブラウザで分析するための
          フロントエンドです。LP / Offer / Cart 検証のための雛形も含みます。
        </p>
      </div>
      <div class="card">
        <h3>主なルート</h3>
        <ul>
          <li>
            <a href="/offers/sample">/offers/:id</a> — Offer 詳細 (擬似)
          </li>
          <li>
            <a href="/cart">/cart</a> — 擬似カート
          </li>
          <li>
            <a href="/thanks">/thanks</a> — リード送信後 Thanks
          </li>
          <li>
            <a href="/admin">/admin</a> — DuckDB 結果ビューア
          </li>
        </ul>
      </div>
    </section>
  );
}
