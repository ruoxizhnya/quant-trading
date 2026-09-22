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

---

## Update (2026-09-22)

**The revisit condition above is now met** — legacy HTML was removed in
[AUD-33](../TASKS.md): the 6 files under `cmd/analysis/static/`, the 10 HTML
routes plus the `/static` mount in `cmd/analysis/main.go`, and the 4 no-prefix
data mirrors in `cmd/analysis/handlers_proxy.go`.

It took **two** changes, in this order — which is the part worth recording:

1. [AUD-32](../TASKS.md) gave the SPA an actual deployment (nginx on host port
   8080). Until then `web/` could only run under `npm run dev` on `:5173`, so the
   legacy pages were the *only* server-side UI — deleting them would have left
   `:8085` serving nothing but API responses.
2. [AUD-33](../TASKS.md) then deleted the legacy pages.

This ADR said "deprecated ... should not be modified" but never scheduled the
removal. That gap — deprecated with no date — is exactly where
`cmd/analysis/static/` sat for five months. `cmd/analysis/deps_test.go` now
carries a `mustNotHave` assertion so the routes cannot quietly come back.
