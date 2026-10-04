package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/buildinfo"
	"github.com/lucaseufrasio/woovi-pix-mcp/internal/config"
)

func TestDoctorUsesProfileWithoutLeakingCredential(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	s, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}

	p := config.Profile{Name: "test", Environment: "simulator", Account: "x", SecretFile: true}
	if err := s.Save(p, "SECRET-DO-NOT-PRINT"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := cli(context.Background(), []string{"doctor", "--profile", "test"}, strings.NewReader(""), &out, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "SECRET-DO-NOT-PRINT") || !strings.Contains(out.String(), "No provider request") {
		t.Fatalf("unsafe doctor: %s", out.String())
	}
}

func TestCLIRejectsUnexpectedSecretArgument(t *testing.T) {
	var out bytes.Buffer

	err := cli(context.Background(), []string{"stdio", "secret-sentinel"}, strings.NewReader(""), &out, log.New(io.Discard, "", 0))
	if err == nil || strings.Contains(err.Error(), "secret-sentinel") || strings.Contains(out.String(), "secret-sentinel") {
		t.Fatal("argument rejection leaked secret")
	}

	err = cli(context.Background(), []string{"stdio", "--secret-sentinel"}, strings.NewReader(""), &out, log.New(io.Discard, "", 0))
	if err == nil || strings.Contains(err.Error(), "secret-sentinel") || strings.Contains(out.String(), "secret-sentinel") {
		t.Fatal("flag error leaked secret")
	}
}

func TestRecoveredStateBlocksCreationWithoutProviderRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	if err := os.WriteFile(path+".recovered", []byte("recovered"), 0600); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{"WOOVI_API_BASE_URL": "http://127.0.0.1:1", "WOOVI_APP_ID": "sentinel", "WOOVI_ENABLE_CHARGE_CREATION": "true", "WOOVI_ACCOUNT_ID": "account", "WOOVI_DATABASE_PATH": path}

	err := run(context.Background(), func(key string) string { return values[key] }, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "blocked after recovery") {
		t.Fatalf("recovery gate: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("blocked startup initialized fresh database")
	}
}

func TestCLIVersionDoesNotRequireCredentials(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		var out bytes.Buffer
		if err := cli(context.Background(), []string{arg}, strings.NewReader(""), &out, log.New(io.Discard, "", 0)); err != nil {
			t.Fatal(err)
		}

		if want := "woovi-pix-mcp " + buildinfo.Version + " (commit " + buildinfo.Commit + ")\n"; out.String() != want {
			t.Fatalf("version %q != %q", out.String(), want)
		}
	}
}
