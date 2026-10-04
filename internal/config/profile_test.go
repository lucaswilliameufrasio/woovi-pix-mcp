package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateProfileRoundTripAndIsolation(t *testing.T) {
	s := Profiles{Root: filepath.Join(t.TempDir(), "config")}
	p := Profile{Name: "sandbox", Environment: "sandbox", Account: "account", Writes: true, SecretFile: true}
	if err := s.Save(p, "secret-sentinel"); err != nil {
		t.Fatal(err)
	}
	got, secret, err := s.Load(p.Name)
	if err != nil || got != p || secret != "secret-sentinel" {
		t.Fatalf("load: %+v %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(s.Root, "profiles", p.Name, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("secret leaked into metadata")
	}
	if err := s.Save(p, "replacement"); err == nil {
		t.Fatal("silent overwrite accepted")
	}
	other := p
	other.Environment = "production"
	if s.DatabasePath(other) == s.DatabasePath(p) {
		t.Fatal("environments share database")
	}
	other = p
	other.Account = "other"
	if s.DatabasePath(other) == s.DatabasePath(p) {
		t.Fatal("accounts share database")
	}
	other = p
	other.Name = "alias"
	if s.DatabasePath(other) != s.DatabasePath(p) {
		t.Fatal("same account/environment must share idempotency state")
	}
	if err := os.Chmod(filepath.Join(s.Root, "profiles", p.Name, "appid"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(p.Name); err == nil {
		t.Fatal("public secret file accepted")
	}
}

func TestInvalidProfilesDoNotWrite(t *testing.T) {
	s := Profiles{Root: filepath.Join(t.TempDir(), "config")}
	for _, p := range []Profile{
		{Name: "../escape", Environment: "sandbox", Account: "x", SecretFile: true},
		{Name: "test", Environment: "unknown", Account: "x", SecretFile: true},
		{Name: "test", Environment: "sandbox", SecretFile: true},
	} {
		if err := s.Save(p, "sentinel"); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if _, err := os.Stat(s.Root); !os.IsNotExist(err) {
		t.Fatal("invalid profile touched filesystem")
	}
}
