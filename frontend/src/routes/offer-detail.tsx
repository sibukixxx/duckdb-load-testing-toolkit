import { useEffect } from 'preact/hooks';
import { useLocation, useRoute } from 'preact-iso';
import { trackCartIntent, trackOfferClick, trackPageView } from '../lib/tracking';

export function OfferDetail() {
  const { params } = useRoute();
  const { route } = useLocation();
  const offerId = params.id ?? 'unknown';

  useEffect(() => {
    trackPageView(`/offers/${offerId}`);
    trackOfferClick(offerId);
  }, [offerId]);

  const handleAddToCart = () => {
    trackCartIntent(offerId);
    route('/cart');
  };

  return (
    <section>
      <div class="card">
        <h2>Offer: {offerId}</h2>
        <p>このページは LP / Offer 検証用の雛形です。実データは未接続。</p>
        <button type="button" onClick={handleAddToCart}>
          カートに追加
        </button>
      </div>
    </section>
  );
}
