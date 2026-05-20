import { useEffect } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { trackLeadSubmitted, trackPageView } from '../lib/tracking';

export function Cart() {
  const { route } = useLocation();

  useEffect(() => {
    trackPageView('/cart');
  }, []);

  const handleSubmit = (event: Event) => {
    event.preventDefault();
    trackLeadSubmitted('cart-default');
    route('/thanks');
  };

  return (
    <section>
      <div class="card">
        <h2>擬似カート</h2>
        <p>リード送信のみを擬似的に行います。</p>
        <form onSubmit={handleSubmit}>
          <button type="submit">リード送信</button>
        </form>
      </div>
    </section>
  );
}
