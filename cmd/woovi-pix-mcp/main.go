package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
	"github.com/lucaseufrasio/woovi-pix-mcp/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := log.New(os.Stderr, "woovi-pix-mcp: ", log.LstdFlags)
	if err := run(context.Background(), os.Getenv, logger); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, logger *log.Logger) error {
	baseURL := strings.TrimSpace(getenv("WOOVI_API_BASE_URL"))
	appID := strings.TrimSpace(getenv("WOOVI_APP_ID"))
	if baseURL == "" || appID == "" {
		return errors.New("WOOVI_API_BASE_URL and WOOVI_APP_ID are required")
	}
	parsedBase, err := url.Parse(baseURL)
	localHTTP := parsedBase != nil && parsedBase.Scheme == "http" && (parsedBase.Hostname() == "127.0.0.1" || parsedBase.Hostname() == "localhost")
	if err != nil || parsedBase == nil || parsedBase.Host == "" || parsedBase.Scheme != "https" && !localHTTP {
		return errors.New("WOOVI_API_BASE_URL must use HTTPS (HTTP is allowed only for localhost simulator)")
	}
	client := charge.NewWooviClient(baseURL, appID, &http.Client{Timeout: 10 * time.Second})
	writeEnabled := strings.EqualFold(strings.TrimSpace(getenv("WOOVI_ENABLE_CHARGE_CREATION")), "true")
	var server *mcpserver.Server
	if writeEnabled {
		databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
		tenant := strings.TrimSpace(getenv("WOOVI_ACCOUNT_ID"))
		if databaseURL == "" || tenant == "" {
			return errors.New("DATABASE_URL and WOOVI_ACCOUNT_ID are required when charge creation is enabled")
		}
		database, store, err := mcpserver.OpenOperationStore(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer database.Close()
		configuredServer, serverErr := mcpserver.NewWithWrites(client, client, store, tenant, true)
		if serverErr != nil {
			return errors.New("unable to initialize MCP server")
		}
		server = configuredServer
	} else {
		configuredServer, serverErr := mcpserver.NewWithWrites(client, client, nil, "", false)
		if serverErr != nil {
			return errors.New("unable to initialize MCP server")
		}
		server = configuredServer
	}
	logger.Print("starting stdio MCP server; provider credentials are not exposed to tools")
	return server.MCP().Run(ctx, &mcp.StdioTransport{})
}
