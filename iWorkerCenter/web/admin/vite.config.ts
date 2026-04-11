import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/admin/',
  build: {
    outDir: 'dist',
  },
  server: {
    proxy: {
      '/api': 'http://localhost:9377',
      '/admin/api': 'http://localhost:9377',
      '/auth': 'http://localhost:9377',
      '/health': 'http://localhost:9377',
      '/v1': 'http://localhost:9377',
    },
  },
});
