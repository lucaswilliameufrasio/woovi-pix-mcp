// Package buildinfo provides release metadata shared by the CLI and MCP server.
package buildinfo

// These values are injected by GoReleaser. Local source builds report dev.
var (
	Version = "dev"
	Commit  = "unknown"
)
