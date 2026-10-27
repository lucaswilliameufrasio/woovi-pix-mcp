# Feature: MCP and agent onboarding

## Status

Implemented and verified locally. Real Claude Code and OpenCode sessions have
not been used for manual host-level validation.

## Confirmed behavior

- First-run setup guides environment, account scope, opt-in charge creation, and
  masked AppID input.
- MCP registration supports OpenCode, Claude Code, Cursor, Codex CLI, and Gemini
  CLI at project scope. Preview precedes writes; existing user configuration is
  preserved and conflicting entries are not overwritten.
- Install a Woovi skill in the current project for repeatable agent guidance.
- Claude Code gets a `SessionStart` reminder that charge creation is a write
  operation and must be explicitly requested. It is guidance, not enforcement.
- OpenCode V2 has no native `SessionStart` event. Its plugin adds the same
  reminder to model context on each call; this does not authorize or block tools.
- Skills and host reminders are project-scoped. Registration defaults to
  project configuration; `--config` can target a different explicitly selected
  file, but the installer never discovers or modifies global configs implicitly.

## Rationale and trade-offs

The Jira MCP separates MCP registration from agent instructions and does not
install hooks. The Device Simulator MCP uses preview-before-apply registration
and does not install lifecycle hooks. Woovi additionally needs a write-safety
reminder because its optional `pix_create_charge` tool can initiate a charge.
The host event models differ, so the Claude Code `SessionStart` hook and
OpenCode model-context plugin are deliberately described as equivalent guidance,
not as a shared lifecycle event or an authorization boundary.

## Verification

Installer tests use disposable project directories and synthetic executable
paths. They verify preview/no-write, preservation of unrelated settings,
refusal to overwrite conflicting managed files, symlink rejection, and
idempotent reapplication. A pseudo-terminal test selects Gemini CLI and checks
that credentials are not copied into its project config. The full repository
gate (`mise exec go@1.27.3 golangci-lint@2.14.0 -- make check`) passed, as did
`git diff --check`. No real AI-client configuration or Woovi API is modified or
contacted by tests. Manual validation in running Claude Code and OpenCode
sessions remains unverified.
