import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';

export default defineConfig({
  plugins: [
    preact({
      prerender: {
        enabled: true,
        renderTarget: '#app',
        additionalPrerenderRoutes: ['/cart', '/thanks', '/admin', '/404'],
      },
    }),
  ],
  server: {
    port: 8080,
  },
  preview: {
    port: 8080,
  },
});
