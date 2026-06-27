/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // NVIDIA 品牌绿（主强调色）
        nv: {
          green: '#76b900',
          'green-bright': '#8ed600',
          'green-dim': '#5a8c00',
          dark: '#000000',
          'dark-50': '#0a0a0a',
          'dark-100': '#0f0f0f',
          'dark-200': '#141414',
          'dark-300': '#1a1a1a',
          'dark-400': '#242424',
        },
        // shadcn 风格中性面（卡片/边框/前景），便于新组件统一观感
        surface: {
          DEFAULT: '#0b0b0c',
          card: '#131316',
          'card-hover': '#18181b',
          border: '#262629',
          'border-hover': '#34343a',
          muted: '#8a8a93',
        },
        // v0.7 语义 token（支持亮/暗双主题；当前 .dark 用暗值，亮主题留扩展钩子）
        fg: { DEFAULT: '#e8e8ec', muted: '#8a8a93' },
        bg: { DEFAULT: '#0b0b0c', card: '#131316', hover: '#18181b' },
        border: { DEFAULT: '#262629', hover: '#34343a' },
      },
      fontFamily: {
        sans: ['Inter', 'PingFang SC', 'Microsoft YaHei', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Consolas', 'monospace'],
      },
      boxShadow: {
        'nv-glow': '0 0 20px rgba(118, 185, 0, 0.3)',
        'card': '0 1px 3px rgba(0, 0, 0, 0.4), 0 1px 2px rgba(0, 0, 0, 0.3)',
        'card-hover': '0 8px 24px rgba(0, 0, 0, 0.5)',
      },
      animation: {
        'pulse-slow': 'pulse 2.5s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'fade-in': 'fadeIn 0.3s ease-out both',
        'slide-up': 'slideUp 0.35s cubic-bezier(0.16, 1, 0.3, 1) both',
        'scale-in': 'scaleIn 0.2s ease-out both',
        'draw': 'drawIn 0.8s ease-out both',
      },
      keyframes: {
        fadeIn: { '0%': { opacity: '0' }, '100%': { opacity: '1' } },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.96)' },
          '100%': { opacity: '1', transform: 'scale(1)' },
        },
        drawIn: {
          '0%': { transform: 'scaleX(0)', transformOrigin: 'left' },
          '100%': { transform: 'scaleX(1)', transformOrigin: 'left' },
        },
      },
      transitionTimingFunction: {
        'out-expo': 'cubic-bezier(0.16, 1, 0.3, 1)',
      },
    },
  },
  plugins: [],
}
