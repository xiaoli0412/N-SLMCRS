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
      },
      fontFamily: {
        sans: ['Inter', 'PingFang SC', 'Microsoft YaHei', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Consolas', 'monospace'],
      },
      boxShadow: {
        'nv-glow': '0 0 20px rgba(118, 185, 0, 0.3)',
        'card': '0 1px 3px rgba(0, 0, 0, 0.4), 0 1px 2px rgba(0, 0, 0, 0.3)',
      },
      animation: {
        'pulse-slow': 'pulse 2.5s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
      },
      keyframes: {
        fadeIn: { '0%': { opacity: '0' }, '100%': { opacity: '1' } },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(8px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
      },
    },
  },
  plugins: [],
}
