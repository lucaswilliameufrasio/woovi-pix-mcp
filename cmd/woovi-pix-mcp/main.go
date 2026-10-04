package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
	"github.com/lucaseufrasio/woovi-pix-mcp/internal/mcpserver"
)

func main() {
	logger := log.New(os.Stderr, "woovi-pix-mcp: ", log.LstdFlags)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli(ctx, os.Args[1:], os.Stdin, os.Stdout, logger); err != nil {
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
	if err != nil || parsedBase == nil || parsedBase.Host == "" || parsedBase.User != nil || parsedBase.RawQuery != "" || parsedBase.Fragment != "" || strings.Trim(parsedBase.Path, "/") != "" || parsedBase.Scheme != "https" && !localHTTP {
		return errors.New("WOOVI_API_BASE_URL must use HTTPS (HTTP is allowed only for localhost simulator)")
	}

	client := charge.NewWooviClient(baseURL, appID, &http.Client{Timeout: 10 * time.Second})
	writeEnabled := strings.EqualFold(strings.TrimSpace(getenv("WOOVI_ENABLE_CHARGE_CREATION")), "true")

	var server *mcpserver.Server

	if writeEnabled {
		tenant := strings.TrimSpace(getenv("WOOVI_ACCOUNT_ID"))
		if tenant == "" {
			return errors.New("WOOVI_ACCOUNT_ID is required when charge creation is enabled")
		}

		path := strings.TrimSpace(getenv("WOOVI_DATABASE_PATH"))
		if path == "" {
			root, err := os.UserConfigDir()
			if err != nil {
				return errors.New("unable to locate local data directory")
			}

			scope := sha256.Sum256([]byte(baseURL + "\x00" + tenant))
			path = filepath.Join(root, "woovi-pix-mcp", "state", hex.EncodeToString(scope[:]), "operations.db")
		}

		if _, err := os.Lstat(path + ".recovered"); !errors.Is(err, os.ErrNotExist) {
			return errors.New("charge creation blocked after recovery; reconcile provider history before explicitly clearing the recovery marker")
		}

		store, err := charge.OpenSQLiteStore(ctx, path)
		if err != nil {
			return err
		}

		defer func() { _ = store.Close() }()

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
