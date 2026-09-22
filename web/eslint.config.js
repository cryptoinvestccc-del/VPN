import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

/**
 * Lint rules for the site.
 *
 * Type-aware rules are on. Without them a linter over a TypeScript
 * project mostly repeats what tsc already said; with them it catches the
 * class of mistake tsc cannot see — a promise nobody awaits, a value
 * narrowed to `any` and then trusted, a condition that is always true.
 * That is the reason to have one here at all.
 *
 * react-hooks is the other reason. Every data path on both pages is a
 * hook, and a missing dependency in an effect is a stale panel that
 * looks like live data — the one failure this site cannot show.
 */
export default tseslint.config(
  { ignores: ['dist', 'node_modules'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      ...tseslint.configs.recommendedTypeChecked,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // An unused argument named with a leading underscore is a
      // deliberate placeholder, not an oversight.
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
    },
  },
  {
    // The config file itself is not part of the app's TS project.
    files: ['eslint.config.js', 'vite.config.ts'],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: { globals: globals.node },
  },
)
