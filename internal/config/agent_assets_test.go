package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAgentAssetsPreservesProjectSettingsAndIsIdempotent(t *testing.T) {
	projectRoot := t.TempDir()

	binary := filepath.Join(projectRoot, "woovi-pix-mcp")
	if err := os.WriteFile(binary, []byte("executable"), 0700); err != nil {
		t.Fatal(err)
	}

	claudeSettingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeSettingsPath), 0755); err != nil {
		t.Fatal(err)
	}

	originalSettings := `{"permissions":{"allow":["Read"]},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[]}]}}`
	if err := os.WriteFile(claudeSettingsPath, []byte(originalSettings), 0600); err != nil {
		t.Fatal(err)
	}

	assets, err := InstallAgentAssets(projectRoot, binary, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, asset := range assets {
		if !asset.Change {
			t.Errorf("preview reported no change for %s", asset.Path)
		}

		if _, err := os.Lstat(asset.Path); err == nil && asset.Path != claudeSettingsPath {
			t.Errorf("preview created project file %s", asset.Path)
		}
	}

	assets, err = InstallAgentAssets(projectRoot, binary, true)
	if err != nil {
		t.Fatal(err)
	}

	for _, asset := range assets {
		if !asset.Change {
			t.Errorf("apply did not install %s", asset.Path)
		}
	}

	settingsBytes, err := os.ReadFile(claudeSettingsPath)
	if err != nil {
		t.Fatal(err)
	}

	settingsInfo, err := os.Stat(claudeSettingsPath)
	if err != nil {
		t.Fatal(err)
	}

	if settingsInfo.Mode().Perm() != 0600 {
		t.Fatalf("Claude settings permissions changed to %v", settingsInfo.Mode().Perm())
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(settings["permissions"]), "Read") || !strings.Contains(string(settings["hooks"]), "SessionStart") || !strings.Contains(string(settings["hooks"]), "hook") {
		t.Fatalf("Claude settings were not merged as expected: %s", settingsBytes)
	}

	backups, err := filepath.Glob(claudeSettingsPath + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("Claude settings backup = %v, err = %v", backups, err)
	}

	backupInfo, err := os.Stat(backups[0])
	if err != nil {
		t.Fatal(err)
	}

	if backupInfo.Mode().Perm() != 0600 {
		t.Fatalf("Claude settings backup permissions = %v", backupInfo.Mode().Perm())
	}

	skillPath := filepath.Join(projectRoot, ".claude", "skills", "woovi-pix-mcp", "SKILL.md")

	skillBytes, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(skillBytes), "financial write") {
		t.Fatal("project skill was not installed")
	}

	pluginPath := filepath.Join(projectRoot, ".opencode", "plugins", "woovi-pix-safety.ts")

	pluginBytes, err := os.ReadFile(pluginPath)
	if err != nil || !strings.Contains(string(pluginBytes), `hook("context"`) || !strings.Contains(string(pluginBytes), "pix_create_charge") {
		t.Fatal("OpenCode context hook was not installed")
	}

	assets, err = InstallAgentAssets(projectRoot, binary, true)
	if err != nil {
		t.Fatal(err)
	}

	for _, asset := range assets {
		if asset.Change {
			t.Errorf("reinstall unexpectedly changed %s", asset.Path)
		}
	}
}

func TestInstallAgentAssetsRejectsSymlinkBeforeWritingAnything(t *testing.T) {
	projectRoot := t.TempDir()

	binary := filepath.Join(projectRoot, "woovi-pix-mcp")
	if err := os.WriteFile(binary, []byte("executable"), 0700); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(projectRoot, ".agents")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if _, err := InstallAgentAssets(projectRoot, binary, true); err == nil {
		t.Fatal("installer accepted a symlinked asset path")
	}

	for _, assetPath := range []string{
		filepath.Join(projectRoot, ".claude", "skills", "woovi-pix-mcp", "SKILL.md"),
		filepath.Join(projectRoot, ".opencode", "plugins", "woovi-pix-safety.ts"),
	} {
		if _, err := os.Lstat(assetPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial installation created %s", assetPath)
		}
	}
}

func TestInstallAgentAssetsRefusesUnmanagedFilesAndMalformedSettings(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		filename string
		contents string
	}{
		{name: "unmanaged skill", filename: ".agents/skills/woovi-pix-mcp/SKILL.md", contents: "# My skill\n"},
		{name: "JSONC settings", filename: ".claude/settings.json", contents: "{ // keep comments\n}\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projectRoot := t.TempDir()

			binary := filepath.Join(projectRoot, "woovi-pix-mcp")
			if err := os.WriteFile(binary, []byte("executable"), 0700); err != nil {
				t.Fatal(err)
			}

			filePath := filepath.Join(projectRoot, filepath.FromSlash(testCase.filename))
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filePath, []byte(testCase.contents), 0644); err != nil {
				t.Fatal(err)
			}

			if _, err := InstallAgentAssets(projectRoot, binary, true); err == nil {
				t.Fatal("unsafe asset installation unexpectedly succeeded")
			}

			actual, err := os.ReadFile(filePath)
			if err != nil || string(actual) != testCase.contents {
				t.Fatal("refused installation modified the existing file")
			}
		})
	}
}
