import { heroui } from '@heroui/react';

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{ts,tsx}',
    './node_modules/@heroui/theme/dist/**/*.{js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Manrope', 'ui-sans-serif', 'system-ui', '-apple-system',
               'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      colors: {
        /* Bumped one step deeper on the slate ramp for readability. ink-4
           (most-used label color) is now slate-500 instead of slate-400, so
           timestamps / IDs / metadata don't visually fade out on glass. */
        ink:    { DEFAULT: '#0f172a', 2: '#1e293b', 3: '#334155', 4: '#64748b', 5: '#94a3b8' },
        brand:  { DEFAULT: '#6366f1', light: '#a5b4fc', deep: '#4338ca' },
      },
      boxShadow: {
        glass: '0 1px 0 rgba(255,255,255,0.8) inset, 0 1px 2px rgba(15,23,42,0.05), 0 12px 32px -8px rgba(67, 56, 202, 0.20)',
        soft: '0 1px 2px rgba(15, 23, 42, 0.04), 0 8px 24px rgba(15, 23, 42, 0.06)',
      },
      backdropBlur: {
        xs: '4px',
      },
    },
  },
  darkMode: 'class',
  plugins: [
    heroui({
      themes: {
        light: {
          colors: {
            background: '#f5f3ff',
            foreground: '#0f172a',
            primary: {
              DEFAULT: '#6366f1',
              foreground: '#ffffff',
            },
            focus: '#6366f1',
            content1: 'rgba(255,255,255,0.72)',
            content2: 'rgba(255,255,255,0.55)',
            content3: 'rgba(255,255,255,0.40)',
            divider: 'rgba(15,23,42,0.08)',
            default: {
              50:  '#f8fafc',
              100: '#f1f5f9',
              200: '#e2e8f0',
              300: '#cbd5e1',
              400: '#94a3b8',
              500: '#64748b',
              600: '#475569',
              700: '#334155',
              800: '#1e293b',
              900: '#0f172a',
              foreground: '#0f172a',
              DEFAULT: '#94a3b8',
            },
            success: { DEFAULT: '#10b981', foreground: '#ffffff' },
            warning: { DEFAULT: '#f59e0b', foreground: '#ffffff' },
            danger:  { DEFAULT: '#ef4444', foreground: '#ffffff' },
          },
          layout: {
            radius: { small: '8px', medium: '12px', large: '18px' },
          },
        },
      },
    }),
  ],
};
