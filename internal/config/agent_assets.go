package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const agentSkill = `---
name: woovi-pix-mcp
description: Use the Woovi Pix MCP safely to inspect Pix charges or create a charge when explicitly requested.
---

<!-- BEGIN WOOVI PIX MCP MANAGED -->
# Woovi Pix MCP

- Use ` + "`pix_get_charge`" + ` to inspect a charge by its Woovi charge ID or correlation ID.
- Treat ` + "`pix_create_charge`" + ` as a financial write: call it only when the user explicitly asks to create a charge and has supplied the requested amount, reference, and expiry.
- Never infer consent to create a charge from a request to inspect, configure, or test the MCP.
- Charge creation may be unavailable because the selected profile is read-only. Do not try to enable it or change profiles without the user's request.
- Never request, display, or copy the AppID into prompts, files, client configuration, or tool output. The MCP process owns the credential.
- A configured sandbox or local simulator is not production. Verify the selected profile before any write operation.
- An MCP tool call or a safety reminder is not a substitute for the user's authorization.
<!-- END WOOVI PIX MCP MANAGED -->
`

const openCodePlugin = `// BEGIN WOOVI PIX MCP MANAGED
import { Plugin } from "@opencode/plugin"

const safetyReminder = "Woovi Pix MCP safety: pix_create_charge may be available for this profile. It is a financial write. Call it only when the user explicitly asks to create a charge. This reminder does not enforce authorization. Never expose or request the AppID."

export default Plugin.define({
  id: "woovi-pix-mcp-safety",
  setup: async (context) => {
    const registration = await context.session.hook("context", (event) => {
      event.system.push({ type: "text", text: safetyReminder })
    })

    return () => registration.dispose()
  },
})
// END WOOVI PIX MCP MANAGED
`

type AgentAsset struct {
	Path   string
	Change bool
}

// InstallAgentAssets previews or installs the Woovi skill and safety reminders
// into a project. It never reads or writes client-global configuration.
func InstallAgentAssets(projectRoot, binary string, apply bool) ([]AgentAsset, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, errors.New("invalid project directory")
	}

	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("project directory does not exist")
	}

	if !filepath.IsAbs(binary) {
		return nil, errors.New("binary must be an absolute path")
	}

	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("binary must be an executable file")
	}

	assets := []AgentAsset{
		{Path: filepath.Join(root, ".agents", "skills", "woovi-pix-mcp", "SKILL.md")},
		{Path: filepath.Join(root, ".claude", "skills", "woovi-pix-mcp", "SKILL.md")},
		{Path: filepath.Join(root, ".opencode", "plugins", "woovi-pix-safety.ts")},
		{Path: filepath.Join(root, ".claude", "settings.json")},
	}
	contents := [][]byte{
		[]byte(agentSkill),
		[]byte(agentSkill),
		[]byte(openCodePlugin),
		nil,
	}

	claudeSettings, err := claudeSettingsWithHook(assets[3].Path, binary)
	if err != nil {
		return nil, err
	}

	contents[3] = claudeSettings

	for index := range assets {
		if err := validateProjectAssetPath(root, assets[index].Path); err != nil {
			return nil, fmt.Errorf("unsafe agent asset destination %s", filepath.Base(assets[index].Path))
		}

		current, readErr := os.ReadFile(assets[index].Path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, fmt.Errorf("read agent asset %s: %w", filepath.Base(assets[index].Path), readErr)
		}

		assets[index].Change = !bytes.Equal(current, contents[index])
		if !assets[index].Change {
			continue
		}

		if readErr == nil && !strings.HasSuffix(assets[index].Path, "settings.json") && !isManagedAsset(string(current), assets[index].Path) {
			return nil, fmt.Errorf("refusing to overwrite unmanaged project file %s", filepath.Base(assets[index].Path))
		}
	}

	if !apply {
		return assets, nil
	}

	for _, asset := range assets {
		if !asset.Change {
			continue
		}

		if _, err := os.Lstat(asset.Path); err == nil {
			if err := backupProjectAsset(asset.Path); err != nil {
				return nil, fmt.Errorf("backup agent asset %s: %w", filepath.Base(asset.Path), err)
			}
		}
	}

	for index, asset := range assets {
		if !asset.Change {
			continue
		}

		if err := writeProjectAsset(asset.Path, contents[index]); err != nil {
			return nil, fmt.Errorf("write agent asset %s: %w", filepath.Base(asset.Path), err)
		}
	}

	return assets, nil
}

func backupProjectAsset(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return errors.New("unable to read original project asset")
	}

	backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".backup-*")
	if err != nil {
		return errors.New("unable to stage project asset backup")
	}

	if err := backup.Chmod(0600); err != nil {
		_ = backup.Close()
		_ = os.Remove(backup.Name())

		return errors.New("unable to restrict project asset backup")
	}

	if _, err := backup.Write(original); err != nil {
		_ = backup.Close()
		_ = os.Remove(backup.Name())

		return errors.New("unable to write project asset backup")
	}

	if err := backup.Sync(); err != nil {
		_ = backup.Close()
		_ = os.Remove(backup.Name())

		return errors.New("unable to sync project asset backup")
	}

	if err := backup.Close(); err != nil {
		_ = os.Remove(backup.Name())
		return errors.New("unable to close project asset backup")
	}

	return nil
}

func claudeSettingsWithHook(path, binary string) ([]byte, error) {
	root := map[string]json.RawMessage{}

	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("unable to read Claude Code settings")
	}

	if len(original) != 0 {
		if err := json.Unmarshal(original, &root); err != nil || root == nil {
			return nil, errors.New("claude code settings must be a JSON object; JSONC is not rewritten")
		}
	}

	hooks := map[string]json.RawMessage{}
	if raw, exists := root["hooks"]; exists {
		if err := json.Unmarshal(raw, &hooks); err != nil || hooks == nil {
			return nil, errors.New("claude code hooks setting must be an object")
		}
	}

	var sessionStart []json.RawMessage
	if raw, exists := hooks["SessionStart"]; exists {
		if err := json.Unmarshal(raw, &sessionStart); err != nil {
			return nil, errors.New("claude code SessionStart hooks must be an array")
		}
	}

	command := map[string]any{
		"type":    "command",
		"command": binary,
		"args":    []string{"hook", "session-start"},
	}
	entry := map[string]any{
		"matcher": "startup|resume|clear|compact|fork",
		"hooks":   []any{command},
	}

	encodedEntry, err := json.Marshal(entry)
	if err != nil {
		return nil, errors.New("unable to encode Claude Code hook")
	}

	for index, existing := range sessionStart {
		var existingEntry struct {
			Matcher string            `json:"matcher"`
			Hooks   []json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(existing, &existingEntry); err != nil {
			return nil, errors.New("invalid Claude Code SessionStart hook entry")
		}

		if existingEntry.Matcher != entry["matcher"] {
			continue
		}

		for hookIndex, existingHookData := range existingEntry.Hooks {
			var existingHook struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}
			if err := json.Unmarshal(existingHookData, &existingHook); err != nil {
				return nil, errors.New("invalid Claude Code SessionStart hook handler")
			}

			if existingHook.Type != "command" || !reflect.DeepEqual(existingHook.Args, []string{"hook", "session-start"}) {
				continue
			}

			if existingHook.Command == binary {
				return original, nil
			}

			encodedHook, err := json.Marshal(command)
			if err != nil {
				return nil, errors.New("unable to encode Claude Code hook")
			}

			existingEntry.Hooks[hookIndex] = encodedHook
			updatedEntry := map[string]any{"matcher": existingEntry.Matcher, "hooks": existingEntry.Hooks}

			sessionStart[index], err = json.Marshal(updatedEntry)
			if err != nil {
				return nil, errors.New("unable to update Claude Code hooks")
			}

			hooks["SessionStart"], err = json.Marshal(sessionStart)
			if err != nil {
				return nil, errors.New("unable to encode Claude Code hooks")
			}

			root["hooks"], err = json.Marshal(hooks)
			if err != nil {
				return nil, errors.New("unable to encode Claude Code settings")
			}

			data, err := json.MarshalIndent(root, "", "  ")
			if err != nil {
				return nil, errors.New("unable to encode Claude Code settings")
			}

			return append(data, '\n'), nil
		}
	}

	sessionStart = append(sessionStart, encodedEntry)

	hooks["SessionStart"], err = json.Marshal(sessionStart)
	if err != nil {
		return nil, errors.New("unable to encode Claude Code hooks")
	}

	root["hooks"], err = json.Marshal(hooks)
	if err != nil {
		return nil, errors.New("unable to encode Claude Code settings")
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, errors.New("unable to encode Claude Code settings")
	}

	return append(data, '\n'), nil
}

func isManagedAsset(contents, path string) bool {
	if strings.HasSuffix(path, "settings.json") {
		return strings.Contains(contents, "woovi-pix-mcp")
	}

	return strings.Contains(contents, "BEGIN WOOVI PIX MCP MANAGED")
}

func writeProjectAsset(path string, contents []byte) error {
	parent := filepath.Dir(path)
	if err := ensureProjectDirectory(parent); err != nil {
		return errors.New("unable to create project asset directory")
	}

	mode := os.FileMode(0644)

	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("project asset destination must be a regular file")
	} else if err == nil {
		mode = info.Mode().Perm()
	}

	temporary, err := os.CreateTemp(parent, ".woovi-agent-*")
	if err != nil {
		return errors.New("unable to stage project asset")
	}

	defer func() { _ = os.Remove(temporary.Name()) }()

	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return errors.New("unable to set project asset permissions")
	}

	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return errors.New("unable to write project asset")
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("unable to sync project asset")
	}

	if err := temporary.Close(); err != nil {
		return errors.New("unable to close staged project asset")
	}

	if err := os.Rename(temporary.Name(), path); err != nil {
		return errors.New("unable to install project asset")
	}

	return nil
}

func ensureProjectDirectory(path string) error {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	missing := []string{}

	current := absolutePath
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("project asset path contains a non-directory or symlink")
			}

			break
		}

		if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}

		missing = append(missing, current)

		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("project asset path has no existing parent")
		}

		current = parent
	}

	for index := len(missing) - 1; index >= 0; index-- {
		if err := os.Mkdir(missing[index], 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}

		info, err := os.Lstat(missing[index])
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("project asset path contains a non-directory or symlink")
		}
	}

	return nil
}

func validateProjectAssetPath(root, path string) error {
	relativePath, err := filepath.Rel(root, path)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return errors.New("project asset destination escapes project root")
	}

	parts := strings.Split(relativePath, string(filepath.Separator))

	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("project asset parent must be a real directory")
		}
	}

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project asset must be a regular file")
	}

	return nil
}
