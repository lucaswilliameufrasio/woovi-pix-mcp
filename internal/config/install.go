package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// InstallMCP edits an explicitly selected JSON client configuration. JSONC is
// rejected rather than losing comments; callers can use the printed snippet.
// An existing entry is only accepted when it already equals our registration.
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
	// Do not create directories or files during a preview.
	var guard *flock.Flock

	if apply {
		if err := privateDir(filepath.Dir(path)); err != nil {
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
	case "claude", "cursor":
		value = map[string]any{"command": binary, "args": []string{"stdio", "--profile", profile}}
	case "opencode":
		value = map[string]any{"type": "local", "command": []string{binary, "stdio", "--profile", profile}}
	default:
		return nil, errors.New("client must be claude, cursor or opencode (V2)")
	}

	return json.Marshal(value)
}
