import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { VitePWA } from 'vite-plugin-pwa';

// 배포 경로가 GCS 슬러그 경로(예: /<bucket>/)이면 VITE_BASE로 넣습니다.
export default defineConfig({
  base: process.env.VITE_BASE ?? '/',
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icon-192.png', 'icon-512.png'],
      manifest: {
        name: '트레이더 설정',
        short_name: '트레이더',
        description: '자동매매 설정 화면',
        display: 'standalone',
        theme_color: '#0f172a',
        background_color: '#0f172a',
        lang: 'ko',
        icons: [
          { src: 'icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      // 상태 파일(status.json)은 SW가 가로채지 않습니다. 앱 셸만 미리 캐시합니다.
      workbox: {
        globPatterns: ['**/*.{js,css,html,png}'],
      },
    }),
  ],
});
