@AGENTS.md

# Claude Code — Specific Configuration

The above @AGENTS.md import provides the full project configuration.
Below are Claude Code-specific additions and overrides.

## Claude-Specific Features

### Subagent Usage
- Use TodoWrite tool for task tracking (mandatory for multi-step tasks)
- Use Task tool with subagent_type="search" for codebase exploration
- Use Task tool with subagent_type="browser_use" for UI testing
- Always use SearchCodebase before making changes to unfamiliar code

### MCP Servers
No MCP servers currently configured.

### Instructions Hierarchy
1. AGENTS.md (imported above) — project-wide rules
2. This file — Claude Code specific behavior
3. User prompt in conversation — task-specific instructions

## Workflow Best Practices for Claude

1. **Plan First**: For complex tasks, always outline approach before coding
2. **Verify**: Run tests after each significant change
3. **Use markRaw()**: Wrap icon components to prevent Vue reactive proxy warnings
4. **Use shallowRef**: For large objects like BacktestResult
5. **await nextTick()**: Before accessing DOM refs after state updates
6. **Document Self-Maintenance**: Follow AGENTS.md "Document Self-Maintenance Protocol" — update docs alongside code changes, create ODRs for operational decisions, never write standalone Reports

---
_Claude Code adapter for Quant Lab project_
_See [AGENTS.md](AGENTS.md) for complete project configuration_
