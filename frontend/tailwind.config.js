/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  // No darkMode toggle — the app is always dark-themed via base colors.
  theme: {
    extend: {
      colors: {
        sentinel: {
          50: "#f0f4ff",
          100: "#dbe4ff",
          300: "#a5b4fc",
          400: "#818cf8",
          500: "#4c6ef5",
          600: "#3b5bdb",
          700: "#364fc7",
          900: "#1b2a4a",
          950: "#0f172a",
        },
        surface: {
          base: "#0D1117",
          raised: "#161B22",
          overlay: "#21262D",
        },
        border: {
          DEFAULT: "#30363D",
        },
        muted: "#8B949E",
        faint: "#6E7681",
        status: {
          ok: "#3FB950",
          warn: "#D29922",
          error: "#F85149",
          info: "#58A6FF",
          accent: "#BC8CFF",
          highlight: "#39D2C0",
        },
      },
    },
  },
  plugins: [],
};
