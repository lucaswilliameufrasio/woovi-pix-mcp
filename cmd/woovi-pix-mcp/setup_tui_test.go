package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSetupWizardShowsProviderURLsAndMasksAppID(t *testing.T) {
	model := newSetupWizardModel("sandbox", false, "https://api.woovi.com/")

	initialView := model.View().Content
	for _, providerURL := range []string{"https://api.woovi-sandbox.com", "https://api.woovi.com", "http://127.0.0.1:8081"} {
		if !strings.Contains(initialView, providerURL) {
			t.Errorf("setup wizard omitted provider URL %q", providerURL)
		}
	}

	if selectedEnvironment := model.environments[model.environment].name; selectedEnvironment != "production" {
		t.Fatalf("expected production URL to preselect its environment, got %q", selectedEnvironment)
	}

	model.step = setupAppIDStep
	model.appIDInput.SetValue("secret-app-id-sentinel")

	appIDView := model.View().Content
	if strings.Contains(appIDView, "secret-app-id-sentinel") || !strings.Contains(appIDView, "••••••••••••••••••••••") {
		t.Fatal("setup wizard did not mask the AppID")
	}
}

func TestInstallClientSelectorSupportsAllDocumentedClients(t *testing.T) {
	model := installClientModel{clients: []string{"opencode", "claude", "cursor", "codex", "gemini"}}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	model = updated.(installClientModel)
	if selected := model.clients[model.selected]; selected != "claude" {
		t.Fatalf("selector moved to %q, want claude", selected)
	}

	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	model = updated.(installClientModel)
	if !model.confirmed {
		t.Fatal("selector did not confirm selected client")
	}
}

func TestDefaultProjectClientConfigMapsAllSupportedClients(t *testing.T) {
	for client, expectedPath := range map[string]string{
		"opencode": "opencode.json",
		"claude":   ".mcp.json",
		"cursor":   filepath.Join(".cursor", "mcp.json"),
		"codex":    filepath.Join(".codex", "config.toml"),
		"gemini":   filepath.Join(".gemini", "settings.json"),
	} {
		path, err := defaultProjectClientConfig(client)
		if err != nil || path != expectedPath {
			t.Errorf("client %s path = %q, err = %v", client, path, err)
		}
	}
}
