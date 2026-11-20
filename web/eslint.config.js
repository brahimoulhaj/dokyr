// `pnpm run lint`. TypeScript already refuses unused names and type errors (tsc, strict);
// this adds what a compiler does not see: the rules of hooks, and a few plain mistakes.
import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['node_modules', 'dist', '../internal/webui/dist'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}', 'vite.config.ts'],
    languageOptions: { globals: globals.browser },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      // Effects here often run once on purpose and read the latest props through a ref.
      'react-hooks/exhaustive-deps': 'off',
      // tsc reports these (noUnusedLocals, noUnusedParameters).
      '@typescript-eslint/no-unused-vars': 'off',
      'no-empty': ['error', { allowEmptyCatch: true }],
    },
  },
)
