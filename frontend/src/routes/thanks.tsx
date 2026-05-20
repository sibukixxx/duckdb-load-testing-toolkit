import { useEffect } from 'preact/hooks';
import { trackPageView } from '../lib/tracking';

export function Thanks() {
  useEffect(() => {
    trackPageView('/thanks');
  }, []);

  return (
    <section>
      <div class="card">
        <h2>Thanks</h2>
        <p>リードを受け付けました（擬似）。</p>
        <p>
          <a href="/">トップへ戻る</a>
        </p>
      </div>
    </section>
  );
}
