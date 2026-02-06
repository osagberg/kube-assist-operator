import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
      colors: {
        glass: {
          50: 'rgba(255, 255, 255, 0.03)',
          100: 'rgba(255, 255, 255, 0.05)',
          200: 'rgba(255, 255, 255, 0.08)',
          300: 'rgba(255, 255, 255, 0.12)',
          400: 'rgba(255, 255, 255, 0.16)',
        },
        edge: {
          DEFAULT: 'rgba(255, 255, 255, 0.06)',
          subtle: 'rgba(255, 255, 255, 0.04)',
          medium: 'rgba(255, 255, 255, 0.08)',
          strong: 'rgba(255, 255, 255, 0.12)',
        },
        severity: {
          critical: 'rgba(239, 68, 68, 0.80)',
          'critical-bg': 'rgba(239, 68, 68, 0.10)',
          'critical-border': 'rgba(239, 68, 68, 0.20)',
          warning: 'rgba(234, 179, 8, 0.80)',
          'warning-bg': 'rgba(234, 179, 8, 0.10)',
          'warning-border': 'rgba(234, 179, 8, 0.20)',
          info: 'rgba(59, 130, 246, 0.80)',
          'info-bg': 'rgba(59, 130, 246, 0.08)',
          'info-border': 'rgba(59, 130, 246, 0.15)',
          healthy: 'rgba(34, 197, 94, 0.80)',
          'healthy-bg': 'rgba(34, 197, 94, 0.10)',
        },
        ai: {
          DEFAULT: 'rgba(168, 85, 247, 0.80)',
          bg: 'rgba(168, 85, 247, 0.10)',
          border: 'rgba(168, 85, 247, 0.20)',
        },
        accent: {
          DEFAULT: '#6366F1',
          muted: 'rgba(99, 102, 241, 0.15)',
          glow: 'rgba(99, 102, 241, 0.25)',
        },
        surface: {
          light: '#FAFAF9',
          'light-card': 'rgba(255, 255, 255, 0.70)',
          'light-hover': 'rgba(0, 0, 0, 0.04)',
          'light-border': 'rgba(0, 0, 0, 0.06)',
        },
      },
      backdropBlur: {
        glass: '20px',
        modal: '40px',
        subtle: '12px',
      },
      boxShadow: {
        glass: '0 4px 24px -1px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(255, 255, 255, 0.05)',
        'glass-sm': '0 2px 8px -1px rgba(0, 0, 0, 0.2), 0 0 0 1px rgba(255, 255, 255, 0.05)',
        'glass-lg': '0 8px 40px -4px rgba(0, 0, 0, 0.4), 0 0 0 1px rgba(255, 255, 255, 0.06)',
        glow: '0 0 20px rgba(99, 102, 241, 0.3)',
        light: '0 1px 3px rgba(0, 0, 0, 0.08), 0 0 0 1px rgba(0, 0, 0, 0.04)',
        'light-lg': '0 4px 16px rgba(0, 0, 0, 0.10), 0 0 0 1px rgba(0, 0, 0, 0.04)',
      },
      animation: {
        'slide-up': 'slide-up 0.3s ease-out',
        'fade-in': 'fade-in 0.2s ease-out',
        'glow-pulse': 'glow-pulse 2s ease-in-out infinite',
      },
      keyframes: {
        'slide-up': {
          '0%': { transform: 'translateY(10px)', opacity: '0' },
          '100%': { transform: 'translateY(0)', opacity: '1' },
        },
        'fade-in': {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        'glow-pulse': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.6' },
        },
      },
    },
  },
  plugins: [],
} satisfies Config
