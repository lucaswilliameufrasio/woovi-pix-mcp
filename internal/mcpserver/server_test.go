package mcpserver

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
)

func TestStdioClientCanListAndCallReadOnlyChargeTool(t *testing.T) {
	provider := httptest.NewServer(charge.NewSimulator())
	defer provider.Close()

	api := charge.NewWooviClient(provider.URL, "simulator", provider.Client())

	_, err := New(api)
	if err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(t.TempDir(), "woovi-pix-mcp")

	build := exec.Command("go", "build", "-o", binary, "../../cmd/woovi-pix-mcp")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server: %v\n%s", err, output)
	}

	command := exec.Command(binary)
	command.Env = append(command.Environ(), "WOOVI_API_BASE_URL="+provider.URL, "WOOVI_APP_ID=simulator")
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)

	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(listed.Tools) != 1 || listed.Tools[0].Name != "pix_get_charge" {
		t.Fatalf("unexpected tools: %+v", listed.Tools)
	}

	if listed.Tools[0].Annotations == nil || !listed.Tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("tool is not declared read-only: %+v", listed.Tools[0].Annotations)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_get_charge", Arguments: map[string]any{"id": "demo-charge"}})
	if err != nil {
		t.Fatal(err)
	}

	if result.IsError || len(result.StructuredContent.(map[string]any)) == 0 {
		t.Fatalf("unexpected call result: %+v", result)
	}

	chargeResult := result.StructuredContent.(map[string]any)
	if chargeResult["id"] != "demo-charge" || chargeResult["amount_cents"] != float64(1250) {
		t.Fatalf("unexpected structured charge: %+v", chargeResult)
	}

	for _, forbidden := range []string{"pix_create_charge", "refund", "transfer", "payout"} {
		for _, tool := range listed.Tools {
			if tool.Name == forbidden {
				t.Fatalf("unexpected non-read-only tool advertised: %s", forbidden)
			}
		}
	}
}
