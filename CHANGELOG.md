# Changelog

Changes to Woovi Pix MCP. Creation is opt-in; no Pix Out, transfers, refunds or
financial cancellations. Entries are generated from Git history and reviewed
in a release pull request before tagging.

## [0.1.2] - 2026-10-05

### Features
- **release:** Add verified platform installers

## [0.1.1] - 2026-10-04


### Maintenance
- License project under Apache 2.0

## [0.1.0] - 2026-10-04


### Bug fixes
- Keep mismatched Woovi charge results unresolved
- Keep mismatched reconciliation unresolved
- Reject trailing Woovi response data
- Upgrade vulnerable Go dependencies


### CI / Release
- Run Go checks with PostgreSQL integration
- Add golangci-lint check
- Update actions and pin Ubuntu runner
- Enable weekly dependency update checks
- Scan Go vulnerabilities in workflow
- Add guarded releases and enforce code readability


### Documentation
- Add README
- Add MCP setup example and harden charge lookup
- Add unreleased changelog and local build steps
- Add local and Woovi sandbox testing walkthroughs


### Features
- Add Woovi Pix MCP charge tools
- Version charge schema and throttle Woovi requests
- Use embedded SQLite for MCP charge operations
- Add CLI profiles and remove unused PostgreSQL infrastructure
- Finish local MCP onboarding and optional Litestream recovery


### Maintenance
- Declare request limiter dependency


### Other
- Use patched Go 1.25 toolchain
- Align Woovi MCP stack with approved versions
- Add development Makefile and timestamped migration
- **deps:** Bump github.com/modelcontextprotocol/go-sdk (#1)
- **deps:** Bump golang.org/x/time from 0.15.0 to 0.16.0 (#2)
- **deps:** Bump github.com/jackc/pgx/v5 from 5.10.0 to 5.11.0 (#3)


### Tests
- Cover invalid cents and malformed Woovi responses
- Verify sanitized Woovi HTTP error mapping
