/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./index.html",
    "./js/**/*.js"
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['IBM Plex Mono', 'ui-monospace', 'SFMono-Regular', 'SF Mono', 'Menlo', 'Consolas', 'monospace'],
        mono: ['IBM Plex Mono', 'ui-monospace', 'SFMono-Regular', 'SF Mono', 'Menlo', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
}
