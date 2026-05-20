import { useEffect } from 'preact/hooks';
import { trackPageView } from '../lib/tracking';

export function NotFound() {
  useEffect(() => {
    trackPageView('/404');
  }, []);

  return (
    <section>
      <div class="card">
        <h2>404 — Not Found</h2>
        <p>ページが見つかりませんでした。</p>
        <p>
          <a href="/">トップへ戻る</a>
        </p>
      </div>
    </section>
  );
}
