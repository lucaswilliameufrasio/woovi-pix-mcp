# Changelog

All notable changes to this project will be documented here.

## Unreleased

- Add a local stdio MCP server with `pix_get_charge` and opt-in
  `pix_create_charge` tools.
- Persist idempotency state and an audit trail in embedded SQLite; reconcile
  uncertain outcomes by correlation ID without blind retries.
- Add a local Woovi HTTP simulator, MCP integration tests, and GitHub Actions
  CI with file-backed SQLite, race detection, formatting, build, and lint checks.
- Add CLI setup, saved profiles, OS credential-vault storage (explicit private
  file fallback), local doctor and stdio profile selection.
- Add explicit client registration preview/apply with backups, and optional
  Litestream file/S3 replication with protected restore and a recovery write gate.
- Reject malformed, mismatched, or trailing provider response data and keep
  uncertain charge operations unresolved.

No release has been published. This project does not perform Pix Out,
transfers, refunds, or financial cancellations.
