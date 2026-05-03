# ADR-011: Vue 3 SPA as Official Frontend (Replacing Legacy HTML)

**Date:** 2026-04-11
**Status:** Accepted

## Context

The project originally used server-rendered HTML pages (`cmd/analysis/static/*.html`) served directly by the analysis-service. As the UI grew more complex (interactive charts, strategy configuration, AI Copilot), the legacy HTML approach became unmaintainable:

1. No component reuse — each page duplicated layout, styling, and API calls
2. No type safety — vanilla JS with no compile-time checks
3. No state management — each page independently fetched data
4. No build tooling — no bundling, tree-shaking, or hot-reload

A Vue 3 SPA was developed incrementally alongside the legacy pages, and is now feature-complete.

## Decision

**Vue 3 SPA is the official frontend.** Legacy HTML pages are deprecated.

- Vue 3 + Composition API + `<script setup lang="ts">`
- Naive UI component library (dark theme)
- Pinia for state management
- Chart.js 4 for data visualization
- Vite dev server (`:5173`) proxies API calls to `:8085`
- Production build served by Nginx or similar static file server

The legacy HTML files remain in `cmd/analysis/static/` for backward compatibility but should not be modified.

## Consequences

- **Positive**: Type-safe frontend, component reuse, hot-reload, tree-shaking, consistent UI
- **Positive**: Clear separation — backend is pure API, frontend is pure SPA
- **Negative**: Requires Node.js build step for production deployment
- **Negative**: Legacy HTML routes still registered in `main.go` (can be removed in future cleanup)

## Review

Revisit when: legacy HTML pages are fully removed from the codebase.
