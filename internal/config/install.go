package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"
)

// InstallMCP edits an explicitly selected JSON client configuration or Codex
// TOML configuration. JSONC is rejected rather than losing comments; callers
// can use the printed registration details. Conflicting entries are not replaced.
func InstallMCP(path, client, binary, profile string, apply bool) (bool, error) {
	if !validName.MatchString(profile) {
		return false, errors.New("invalid profile name")
	}

	if !filepath.IsAbs(binary) {
		return false, errors.New("binary must be an absolute path")
	}

	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return false, errors.New("binary must be an executable file")
	}

	entry, err := MCPEntry(client, binary, profile)
	if err != nil {
		return false, err
	}

	if client == "codex" {
		return installCodexMCP(path, binary, profile, apply)
	}
	// Do not create directories or files during a preview.
	var guard *flock.Flock

	if apply {
		if err := clientConfigDir(filepath.Dir(path)); err != nil {
			return false, err
		}

		guard = flock.New(path+".lock", flock.SetPermissions(0600))
		if err := guard.Lock(); err != nil {
			return false, errors.New("unable to lock client configuration")
		}

		defer func() { _ = guard.Unlock() }()
	}

	original, err := readPrivate(path)
	if err != nil {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			return false, err
		}

		original = nil
	}

	root := map[string]json.RawMessage{}
	if len(original) > 0 {
		if err := json.Unmarshal(original, &root); err != nil || root == nil {
			return false, errors.New("client configuration must be a JSON object; JSONC is not rewritten")
		}
	}

	container := root

	key := "mcpServers"
	if client == "opencode" {
		key = "servers"

		container = map[string]json.RawMessage{}
		if raw, ok := root["mcp"]; ok {
			if err := json.Unmarshal(raw, &container); err != nil || container == nil {
				return false, errors.New("invalid mcp configuration")
			}
		}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := container[key]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return false, errors.New("invalid MCP servers configuration")
		}
	}

	name := "woovi-pix-" + profile
	if old, ok := servers[name]; ok {
		var compact bytes.Buffer
		if err := json.Compact(&compact, old); err != nil {
			return false, errors.New("invalid existing registration")
		}

		if bytes.Equal(compact.Bytes(), entry) {
			return false, nil
		}

		return false, errors.New("registration already exists with different settings; no overwrite performed")
	}

	servers[name] = entry

	container[key], err = json.Marshal(servers)
	if err != nil {
		return false, err
	}

	if client == "opencode" {
		root["mcp"], err = json.Marshal(container)
		if err != nil {
			return false, err
		}
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}

	if !apply {
		return true, nil
	}

	if len(original) > 0 {
		backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".backup-*")
		if err != nil {
			return false, errors.New("unable to create client backup")
		}

		_, writeErr := backup.Write(original)
		syncErr := backup.Sync()

		closeErr := backup.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return false, errors.New("unable to persist client backup")
		}
	}

	if err := writePrivate(path, append(data, '\n')); err != nil {
		return false, err
	}

	return true, nil
}

func MCPEntry(client, binary, profile string) (json.RawMessage, error) {
	if !validName.MatchString(profile) {
		return nil, errors.New("invalid profile name")
	}

	var value any

	switch client {
	case "claude", "cursor", "gemini":
		value = map[string]any{"command": binary, "args": []string{"stdio", "--profile", profile}}
	case "opencode":
		value = map[string]any{"type": "local", "command": []string{binary, "stdio", "--profile", profile}}
	case "codex":
		value = map[string]any{"command": binary, "args": []string{"stdio", "--profile", profile}}
	default:
		return nil, errors.New("client must be claude, cursor, opencode, codex or gemini")
	}

	return json.Marshal(value)
}

func installCodexMCP(path, binary, profile string, apply bool) (bool, error) {
	var guard *flock.Flock

	if apply {
		if err := clientConfigDir(filepath.Dir(path)); err != nil {
			return false, err
		}

		guard = flock.New(path+".lock", flock.SetPermissions(0600))
		if err := guard.Lock(); err != nil {
			return false, errors.New("unable to lock client configuration")
		}

		defer func() { _ = guard.Unlock() }()
	}

	original, err := readPrivate(path)
	if err != nil {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			return false, err
		}

		original = nil
	}

	decoded := map[string]any{}
	if len(original) > 0 {
		if err := toml.Unmarshal(original, &decoded); err != nil {
			return false, errors.New("codex configuration must be valid TOML")
		}
	}

	servers, err := tomlTable(decoded, "mcp_servers")
	if err != nil {
		return false, err
	}

	entryName := "woovi-pix-" + profile
	expected := map[string]any{"command": binary, "args": []any{"stdio", "--profile", profile}}

	if existing, exists := servers[entryName]; exists {
		entry, ok := existing.(map[string]any)
		if ok && reflect.DeepEqual(entry, expected) {
			return false, nil
		}

		return false, errors.New("codex registration exists with different settings; no overwrite performed")
	}

	addition := fmt.Sprintf("\n[mcp_servers.%s]\ncommand = %s\nargs = [\"stdio\", \"--profile\", %s]\n", entryName, codexTOMLString(binary), codexTOMLString(profile))

	updated := append(append([]byte(nil), original...), []byte(addition)...)
	if len(original) == 0 {
		updated = []byte(strings.TrimPrefix(addition, "\n"))
	}

	if !apply {
		return true, nil
	}

	if len(original) > 0 {
		backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".backup-*")
		if err != nil {
			return false, errors.New("unable to create client backup")
		}

		_, writeErr := backup.Write(original)
		syncErr := backup.Sync()

		closeErr := backup.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return false, errors.New("unable to persist client backup")
		}
	}

	if err := writePrivate(path, updated); err != nil {
		return false, err
	}

	return true, nil
}

func tomlTable(root map[string]any, key string) (map[string]any, error) {
	if value, exists := root[key]; exists {
		if table, ok := value.(map[string]any); ok {
			return table, nil
		}

		return nil, fmt.Errorf("codex `%s` setting must be a TOML table", key)
	}

	return map[string]any{}, nil
}

func codexTOMLString(value string) string {
	return strconv.Quote(value)
}

func clientConfigDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.New("unable to create client configuration directory")
	}

	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("client configuration directory must not be writable by other users or be a symlink")
	}

	return nil
}
