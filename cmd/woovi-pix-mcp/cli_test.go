package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"testing"

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
}
