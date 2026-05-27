import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/ws': {
        target: 'wss://localhost:8443',
        ws: true,
        secure: false,
      },
    },
  },
})
