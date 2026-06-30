// eslint.config.js — S7-P2-6 (ODR-043-5)
//
// Flat config for ESLint 9+ / Vue 3 / TypeScript. Replaces the missing
// `.eslintrc.*` referenced by AGENTS.md §5/§9 so `npm run lint` works
// for the first time on this project.
//
// Scope: Vue 3 SFCs + TypeScript + Vitest specs. Prettier is intentionally
// NOT integrated here (out of scope for S7-P2-6); a future task can add
// eslint-config-prettier if formatting conflicts arise.
//
// References:
//   - https://eslint.org/docs/latest/use/configure/configuration-files
//   - https://eslint.vuejs.org/user-guide/#flat-config
//   - https://typescript-eslint.io/getting-started

import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'
import vueParser from 'vue-eslint-parser'

export default [
  // ============================================================
  // Global ignores — keep the lint surface small and fast.
  // ============================================================
  {
    ignores: [
      'dist/**',
      'node_modules/**',
      'coverage/**',
      '*.config.{js,ts,cjs,mjs}',
      'scripts/**',
      'src/env.d.ts', // Vite/Vue boilerplate declaration file
    ],
  },

  // ============================================================
  // Base: JS recommended.
  // ============================================================
  js.configs.recommended,

  // ============================================================
  // TypeScript: recommended type-aware-less rules (fast, no program
  // needed). typecheck is delegated to `vue-tsc --noEmit`.
  // ============================================================
  ...tseslint.configs.recommended,

  // ============================================================
  // Vue 3: recommended rules + vue-eslint-parser.
  // ============================================================
  ...pluginVue.configs['flat/recommended'],

  // ============================================================
  // Vue SFC files: route <script> blocks through the TS parser so
  // that type-aware syntax (generics, interfaces, type imports) is
  // linted correctly.
  // ============================================================
  {
    files: ['**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: {
        parser: tseslint.parser,
        sourceType: 'module',
        ecmaVersion: 'latest',
        extraFileExtensions: ['.vue'],
      },
    },
  },

  // ============================================================
  // All source files: project conventions.
  // ============================================================
  {
    files: ['src/**/*.{ts,tsx,vue}'],
    languageOptions: {
      sourceType: 'module',
      ecmaVersion: 'latest',
      globals: {
        // Vitest globals (test, describe, expect, vi, ...)
        test: 'readonly',
        describe: 'readonly',
        it: 'readonly',
        expect: 'readonly',
        beforeEach: 'readonly',
        afterEach: 'readonly',
        beforeAll: 'readonly',
        afterAll: 'readonly',
        vi: 'readonly',
        // Browser globals — SFCs use window/document/console/etc. directly.
        window: 'readonly',
        document: 'readonly',
        console: 'readonly',
        navigator: 'readonly',
        location: 'readonly',
        history: 'readonly',
        URL: 'readonly',
        Blob: 'readonly',
        HTMLElement: 'readonly',
        HTMLDivElement: 'readonly',
        HTMLCanvasElement: 'readonly',
        DragEvent: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        fetch: 'readonly',
      },
    },
    rules: {
      // Vue 3 <script setup> uses top-level await and unused vars are
      // frequently intentional (template bindings).
      'vue/multi-word-component-names': 'off',
      'no-unused-vars': 'off', // defer to @typescript-eslint/no-unused-vars
      // TS compiler already catches undefined vars; the core no-undef
      // rule produces false positives for browser globals in .vue SFCs.
      'no-undef': 'off',
      '@typescript-eslint/no-unused-vars': [
        'warn',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_|^[A-Z][A-Za-z0-9]*$',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      // Allow console.* in dev; production builds strip via Vite.
      'no-console': 'off',
      // AGENTS.md §9 allows `any` with explicit comment. Existing codebase
      // has many `Record<string, any>` prop types; downgrading to warning
      // surfaces new usages for review without blocking lint on existing
      // code. Future task can tighten to error after cleanup.
      '@typescript-eslint/no-explicit-any': 'warn',
      // Same rationale — `{}` empty object types are common in legacy
      // Vue 3 DefineComponent boilerplate. Warn, don't error.
      '@typescript-eslint/no-empty-object-type': 'warn',
    },
  },

  // ============================================================
  // Test files: relax some rules that conflict with vitest patterns.
  // ============================================================
  {
    files: ['src/**/*.{test,spec}.{ts,tsx}'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
    },
  },
]
