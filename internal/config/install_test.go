package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPreservesBacksUpAndIsIdempotent(t *testing.T) {
	for _, client := range []string{"claude", "cursor", "opencode"} {
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
