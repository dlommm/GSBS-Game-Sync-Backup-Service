/** @type {import('tailwindcss').Config} */
// The UI is styled by the handwritten component CSS in static/src/input.css
// (no Tailwind utilities in any template — Tailwind contributes only the
// preflight reset + minify). The palette below mirrors the LOCKED v5 design
// tokens (input.css :root) so an accidentally emitted utility can never
// reintroduce the retired pre-v5 indigo theme.
module.exports = {
  content: [
    './templates/**/*.html',
    '../../client/webui/templates/**/*.html',
  ],
  theme: {
    extend: {
      colors: {
        bg: '#101413',
        'bg-raised': '#151917',
        surface: '#1e2321',
        'surface-hover': '#242a27',
        border: '#2a302d',
        'border-focus': '#3fbfae',
        text: '#eef2f0',
        'text-secondary': '#a8b3ae',
        'text-muted': '#8a948f',
        accent: '#3fbfae',
        'accent-hover': '#55cdbc',
        success: '#2ec27e',
        warning: '#eab54e',
        error: '#e5605e',
        info: '#4cc3e0',
      },
      fontFamily: {
        sans: ['"DM Sans"', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"SF Mono"', 'Consolas', 'monospace'],
      },
      borderRadius: {
        DEFAULT: '12px',
        sm: '8px',
        xs: '4px',
      },
    },
  },
  plugins: [],
};
