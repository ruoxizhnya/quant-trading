// Package agents implements the Go-native AI research agents (Research,
// Generate, Validate, Evolve) for the Phase 4 quantitative research pipeline.
//
// # Deprecated
//
// Per ODR-046 (2026-07-02), this package's Go-native agents are superseded by
// the Hermes Agent autonomous research layer. The primary research workflow
// is now:
//
//	Hermes Agent → MCP bridge (18 tools) → Go backend
//
// The Hermes Agent provides a natural-language interaction model that replaces
// the originally planned frontend AI UI (which was never created — see
// ODR-045). These Go agents are retained for backward compatibility and are
// still exercised by existing tests, but should NOT be used in new code.
//
// New research flows should use the MCP tool layer in pkg/tools/builtin/ and
// the Hermes Skill defined at docs/hermes/skills/autonomous_factor_mining.md.
//
// References:
//   - docs/odr/odr-045-frontend-ai-component-deprecation.md
//   - docs/odr/odr-046-hermes-agent-integration-decision.md
//   - docs/hermes/skills/autonomous_factor_mining.md
//   - docs/hermes/config/hermes.yaml
package agents
