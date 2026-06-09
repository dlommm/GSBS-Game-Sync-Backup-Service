/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './templates/**/*.html',
    '../../client/webui/templates/**/*.html',
  ],
  theme: {
    extend: {
      colors: {
        bg: '#09090b',
        'bg-raised': '#0f0f14',
        surface: '#16161d',
        'surface-hover': '#1c1c26',
        border: '#27272f',
        'border-focus': '#3b3b4a',
        text: '#f4f4f8',
        'text-secondary': '#a1a1b5',
        'text-muted': '#64647a',
        accent: '#6366f1',
        'accent-hover': '#818cf8',
        success: '#22c55e',
        warning: '#eab308',
        error: '#ef4444',
        info: '#38bdf8',
      },
      fontFamily: {
        sans: ['"DM Sans"', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"SF Mono"', 'Consolas', 'monospace'],
      },
      borderRadius: {
        DEFAULT: '10px',
        sm: '6px',
        xs: '4px',
      },
    },
  },
  plugins: [],
};
