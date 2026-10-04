package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/zalando/go-keyring"
)

type Profile struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Account     string `json:"account"`
	Writes      bool   `json:"writes"`
	SecretFile  bool   `json:"secret_file"`
}

type Profiles struct{ Root string }

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func Default() (Profiles, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return Profiles{}, errors.New("unable to locate configuration directory")
	}
	return Profiles{Root: filepath.Join(root, "woovi-pix-mcp")}, nil
}

func (p Profile) BaseURL() string {
	switch p.Environment {
	case "sandbox":
		return "https://api.woovi-sandbox.com"
	case "production":
		return "https://api.woovi.com"
	case "simulator":
		return "http://127.0.0.1:8081"
	default:
		return ""
	}
}

func (p Profile) Validate() error {
	if !validName.MatchString(p.Name) {
		return errors.New("profile name must contain only letters, digits, hyphen or underscore")
	}
	if p.BaseURL() == "" {
		return errors.New("environment must be sandbox, production or simulator")
	}
	if p.Account == "" || len(p.Account) > 256 {
		return errors.New("an account identifier is required")
	}
	return nil
}

func (s Profiles) dir(name string) (string, error) {
	if !validName.MatchString(name) {
		return "", errors.New("invalid profile name")
	}
	return filepath.Join(s.Root, "profiles", name), nil
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("unable to create private configuration directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("configuration directory must be private (0700) and not a symlink")
	}
	return nil
}

func readPrivate(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("configuration file missing or not private (0600)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("unable to read private configuration")
	}
	return data, nil
}

func writePrivate(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return errors.New("refusing unsafe configuration destination")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return errors.New("unable to stage configuration")
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return errors.New("unable to write configuration")
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return errors.New("unable to sync configuration")
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return errors.New("unable to save configuration")
	}
	return nil
}

func (s Profiles) secretID(name string) string {
	abs, _ := filepath.Abs(s.Root)
	sum := sha256.Sum256([]byte(abs + "\x00" + name))
	return hex.EncodeToString(sum[:])
}

func (s Profiles) Save(p Profile, secret string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if secret == "" {
		return errors.New("AppID must not be empty")
	}
	dir, err := s.dir(p.Name)
	if err != nil {
		return err
	}
	if err := privateDir(s.Root); err != nil {
		return err
	}
	if err := privateDir(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	// Editing existing profiles is deliberately deferred: changing the account
	// must never silently reuse another account's credential or database.
	if _, err := os.Lstat(filepath.Join(dir, "profile.json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("profile already exists; choose a new profile name")
	}
	if p.SecretFile {
		if err := writePrivate(filepath.Join(dir, "appid"), []byte(secret)); err != nil {
			return err
		}
	} else if err := keyring.Set("woovi-pix-mcp", s.secretID(p.Name), secret); err != nil {
		return errors.New("OS credential vault unavailable; configure it or explicitly choose --secret-file")
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, "profile.json"), data)
}

func (s Profiles) Load(name string) (Profile, string, error) {
	dir, err := s.dir(name)
	if err != nil {
		return Profile{}, "", err
	}
	data, err := readPrivate(filepath.Join(dir, "profile.json"))
	if err != nil {
		return Profile{}, "", err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return p, "", errors.New("invalid profile configuration")
	}
	if err := p.Validate(); err != nil {
		return p, "", err
	}
	if p.Name != name {
		return p, "", errors.New("profile identity mismatch")
	}
	var secret string
	if p.SecretFile {
		data, err = readPrivate(filepath.Join(dir, "appid"))
		secret = string(data)
	} else {
		secret, err = keyring.Get("woovi-pix-mcp", s.secretID(name))
	}
	if err != nil || secret == "" {
		return p, "", errors.New("profile credential unavailable")
	}
	return p, secret, nil
}

func (s Profiles) DatabasePath(p Profile) string {
	sum := sha256.Sum256([]byte(p.BaseURL() + "\x00" + p.Account))
	return filepath.Join(s.Root, "state", hex.EncodeToString(sum[:]), "operations.db")
}
