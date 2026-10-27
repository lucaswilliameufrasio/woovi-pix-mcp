package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPreservesBacksUpAndIsIdempotent(t *testing.T) {
	for _, client := range []string{"claude", "cursor", "opencode", "gemini"} {
		t.Run(client, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}

			binary := filepath.Join(dir, "binary")
			if err := os.WriteFile(binary, []byte("binary"), 0700); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(dir, "client.json")

			original := []byte(`{"other":{"keep":true},"mcpServers":{"other":{"command":"keep"}},"mcp":{"extra":42,"servers":{"other":{"type":"local","command":["keep"]}}}}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}

			if changed, err := InstallMCP(path, client, binary, "sandbox", false); err != nil || !changed {
				t.Fatalf("preview %v %v", changed, err)
			}

			before, _ := os.ReadFile(path)
			if !bytes.Equal(before, original) {
				t.Fatal("preview changed file")
			}

			if changed, err := InstallMCP(path, client, binary, "sandbox", true); err != nil || !changed {
				t.Fatalf("apply %v %v", changed, err)
			}

			backups, err := filepath.Glob(path + ".backup-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups %v %v", backups, err)
			}

			backup, _ := os.ReadFile(backups[0])
			if !bytes.Equal(backup, original) {
				t.Fatal("backup mismatch")
			}

			data, _ := os.ReadFile(path)

			var root map[string]json.RawMessage
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(root["other"], []byte("{\n    \"keep\": true\n  }")) {
				var v map[string]bool
				if err := json.Unmarshal(root["other"], &v); err != nil || !v["keep"] {
					t.Fatal("lost unrelated config")
				}
			}

			if changed, err := InstallMCP(path, client, binary, "sandbox", true); err != nil || changed {
				t.Fatalf("replay %v %v", changed, err)
			}

			if err := os.WriteFile(binary+"2", nil, 0700); err != nil {
				t.Fatal(err)
			}

			if _, err := InstallMCP(path, client, binary+"2", "sandbox", true); err == nil {
				t.Fatal("conflicting registration overwritten")
			}

			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("conflict changed config")
			}
		})
	}
}

func TestCodexInstallPreservesTOMLAndRefusesConflictingEntry(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join(directory, "woovi binary")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(directory, "config.toml")

	original := "# Keep this comment\nmodel = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other\"\nargs = []\n"
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	changed, err := InstallMCP(configPath, "codex", binaryPath, "sandbox", false)
	if err != nil || !changed {
		t.Fatalf("preview changed=%t err=%v", changed, err)
	}

	preview, err := os.ReadFile(configPath)
	if err != nil || string(preview) != original {
		t.Fatal("Codex preview modified configuration")
	}

	changed, err = InstallMCP(configPath, "codex", binaryPath, "sandbox", true)
	if err != nil || !changed {
		t.Fatalf("apply changed=%t err=%v", changed, err)
	}

	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(updated, []byte(original)) || !bytes.Contains(updated, []byte("[mcp_servers.woovi-pix-sandbox]")) {
		t.Fatalf("Codex config did not preserve original and append registration: %s", updated)
	}

	changed, err = InstallMCP(configPath, "codex", binaryPath, "sandbox", true)
	if err != nil || changed {
		t.Fatalf("idempotent apply changed=%t err=%v", changed, err)
	}

	conflicting := strings.Replace(string(updated), "woovi binary", "other-binary", 1)
	if err := os.WriteFile(configPath, []byte(conflicting), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallMCP(configPath, "codex", binaryPath, "sandbox", true); err == nil {
		t.Fatal("conflicting Codex registration was overwritten")
	}

	actual, err := os.ReadFile(configPath)
	if err != nil || string(actual) != conflicting {
		t.Fatal("conflicting Codex registration changed configuration")
	}
}

func TestInstallRejectsUnsafeOrInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()

	binary := filepath.Join(dir, "binary")
	if err := os.WriteFile(binary, nil, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "client.json")
	for _, data := range []string{"null", "[]", "{ // comment\n}", `{"mcpServers":null}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := InstallMCP(path, "claude", binary, "test", true); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}

	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallMCP(path, "claude", binary, "test", true); err == nil {
		t.Fatal("accepted public configuration")
	}
}

func TestProjectClientConfigCanUseAnExistingSharedReadOnlyDirectory(t *testing.T) {
	projectRoot := t.TempDir()

	configDirectory := filepath.Join(projectRoot, ".cursor")
	if err := os.Mkdir(configDirectory, 0755); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(projectRoot, "woovi-pix-mcp")
	if err := os.WriteFile(binary, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(configDirectory, "mcp.json")
	if changed, err := InstallMCP(configPath, "cursor", binary, "sandbox", true); err != nil || !changed {
		t.Fatalf("install changed=%t err=%v", changed, err)
	}

	info, err := os.Stat(configDirectory)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("installer unexpectedly changed project directory permissions: %v", err)
	}

	configInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if configInfo.Mode().Perm() != 0600 {
		t.Fatalf("client config permissions = %v", configInfo.Mode().Perm())
	}
}
