import { LocationProvider, Router, Route, ErrorBoundary, lazy } from 'preact-iso';
import { Header } from './components/header';
import { Home } from './routes/home';
import { NotFound } from './routes/not-found';

const OfferDetail = lazy(() => import('./routes/offer-detail').then((m) => m.OfferDetail));
const Cart = lazy(() => import('./routes/cart').then((m) => m.Cart));
const Thanks = lazy(() => import('./routes/thanks').then((m) => m.Thanks));
const Admin = lazy(() => import('./routes/admin').then((m) => m.Admin));

export function App() {
  return (
    <LocationProvider>
      <div class="app-shell">
        <Header />
        <main class="app-main">
          <ErrorBoundary>
            <Router>
              <Route path="/" component={Home} />
              <Route path="/offers/:id" component={OfferDetail} />
              <Route path="/cart" component={Cart} />
              <Route path="/thanks" component={Thanks} />
              <Route path="/admin" component={Admin} />
              <Route default component={NotFound} />
            </Router>
          </ErrorBoundary>
        </main>
      </div>
    </LocationProvider>
  );
}
