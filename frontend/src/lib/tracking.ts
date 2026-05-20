import { postEvent } from './api';

type TrackingEvent =
  | { name: 'page_view'; payload: { path: string } }
  | { name: 'offer_click'; payload: { offerId: string } }
  | { name: 'cart_intent'; payload: { offerId: string } }
  | { name: 'lead_submitted'; payload: { offerId: string } };

const SHOULD_FORWARD = false;

function dispatch(event: TrackingEvent): void {
  if (typeof window === 'undefined') {
    return;
  }
  // eslint-disable-next-line no-console
  console.info('[track]', event.name, event.payload);
  if (SHOULD_FORWARD) {
    void postEvent(event.name, event.payload as Record<string, unknown>);
  }
}

export function trackPageView(path: string): void {
  dispatch({ name: 'page_view', payload: { path } });
}

export function trackOfferClick(offerId: string): void {
  dispatch({ name: 'offer_click', payload: { offerId } });
}

export function trackCartIntent(offerId: string): void {
  dispatch({ name: 'cart_intent', payload: { offerId } });
}

export function trackLeadSubmitted(offerId: string): void {
  dispatch({ name: 'lead_submitted', payload: { offerId } });
}
