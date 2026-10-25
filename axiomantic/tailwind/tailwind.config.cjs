// The designer's palette, sizes and radii as CSS variables: the panel's
// «Дизайн» section overrides them (build.py writes the overrides), so one
// compiled stylesheet serves every colour choice.
const v = (name) => `rgb(var(--c-${name}) / <alpha-value>)`;
module.exports = {
  content: ['../build.py', '../site/assets/js/*.js'],
  theme: {
    extend: {
      fontFamily: { sans: ['Inter', 'Inter Fallback', 'system-ui', 'sans-serif'] },
      colors: {
        brand: { DEFAULT: v('brand'), hover: v('brand-hover'), light: v('brand-light') },
        surface: { DEFAULT: v('surface'), muted: v('surface-muted') },
        text: { heading: v('heading'), body: v('body'), muted: v('muted') },
        dark: { DEFAULT: v('dark'), text: v('dark-text') }
      },
      borderRadius: { card: 'var(--r-card)', section: 'var(--r-section)', inner: 'var(--r-inner)' },
      boxShadow: {
        soft: '0 8px 30px rgb(0,0,0,0.04)',
        hover: '0 20px 60px rgb(var(--c-brand) / 0.10)'
      },
      maxWidth: { container: 'var(--w-container)' }
    }
  }
};
