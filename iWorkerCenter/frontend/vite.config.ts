import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/admin/',
  server: {
    proxy: {
      '/admin': {
        target: 'http://127.0.0.1:9377',
        changeOrigin: true,
      },
      '/auth': {
        target: 'http://127.0.0.1:9377',
        changeOrigin: true,
      },
    },
  },
});
