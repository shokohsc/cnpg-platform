/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        bg: '#0f1115',
        panel: '#171a21',
        panel2: '#1e222b',
        border: '#2a2f3a',
        accent: '#3ecf8e',
        accentDim: '#2a8f63',
        fg: '#d7dce2',
        dim: '#8b93a1'
      }
    }
  },
  plugins: []
}
