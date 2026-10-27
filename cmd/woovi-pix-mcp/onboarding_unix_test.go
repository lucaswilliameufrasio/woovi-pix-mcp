//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/config"
)

func TestRealTerminalSetupAndProfileStdio(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "test-binary")

	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "setup", "--profile", "sandbox", "--secret-file")

	cmd.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+root)

	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = terminal.Close() }()

	output := make(chan []byte, 32)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := terminal.Read(buf)
			if n > 0 {
				output <- append([]byte(nil), buf[:n]...)
			}

			if err != nil {
				close(output)
				return
			}
		}
	}()

	var transcript bytes.Buffer

	waitFor := func(prompt string) {
		t.Helper()

		for !strings.Contains(transcript.String(), prompt) {
			select {
			case data, ok := <-output:
				if !ok {
					t.Fatal("terminal ended before expected prompt")
				}

				transcript.Write(data)
			case <-ctx.Done():
				t.Fatalf("terminal prompt %q timed out; transcript: %s", prompt, strings.ReplaceAll(transcript.String(), "tty-secret-sentinel", "[redacted]"))
			}
		}
	}
	for _, step := range []struct{ prompt, answer string }{
		{"Escolha o ambiente", "\r"},
		{"Identificador estável da conta", "test-account\r"},
		{"Permitir criação de cobranças Pix?", "\r"},
		{"AppID da conta selecionada", "tty-secret-sentinel\r"},
	} {
		waitFor(step.prompt)

		if _, err := io.WriteString(terminal, step.answer); err != nil {
			t.Fatal(err)
		}
	}

	if err := cmd.Wait(); err != nil {
		for data := range output {
			transcript.Write(data)
		}

		t.Fatalf("setup failed: %v %s", err, strings.ReplaceAll(transcript.String(), "tty-secret-sentinel", "[redacted]"))
	}

	for data := range output {
		transcript.Write(data)
	}

	if strings.Contains(transcript.String(), "tty-secret-sentinel") {
		t.Fatal("secret echoed in terminal output")
	}

	if !strings.Contains(transcript.String(), "Perfil salvo.") {
		t.Fatal("first-run setup did not confirm profile creation")
	}

	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)

	profiles, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}

	_, secret, err := profiles.Load("sandbox")
	if err != nil || secret != "tty-secret-sentinel" {
		t.Fatal("profile did not persist hidden input")
	}

	command := exec.CommandContext(ctx, binary, "stdio", "--profile", "sandbox")
	// Deliberately poisoned environment values must not override a saved profile.
	command.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+root, "WOOVI_ENABLE_CHARGE_CREATION=true", "WOOVI_APP_ID=wrong")
	client := mcp.NewClient(&mcp.Implementation{Name: "onboarding-test", Version: "1"}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "pix_get_charge" {
		t.Fatal("profile stdio did not preserve read-only configuration")
	}
}

func TestPartialEnvironmentStartsFirstRunWizard(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "test-binary")

	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v %s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, binary)

	command.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+root, "WOOVI_API_BASE_URL=https://api.woovi.com", "WOOVI_APP_ID=")

	terminal, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close() }()

	output := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 1024)
		for {
			count, readError := terminal.Read(buffer)
			if count > 0 {
				output <- append([]byte(nil), buffer[:count]...)
			}

			if readError != nil {
				close(output)
				return
			}
		}
	}()

	var transcript strings.Builder
	for !strings.Contains(transcript.String(), "❯ Produção") {
		select {
		case data, open := <-output:
			if !open {
				t.Fatalf("terminal ended before setup selection: %s", transcript.String())
			}

			transcript.Write(data)
		case <-ctx.Done():
			t.Fatalf("setup wizard did not start for partial environment: %s", transcript.String())
		}
	}

	if strings.Contains(transcript.String(), "WOOVI_API_BASE_URL and WOOVI_APP_ID are required") {
		t.Fatal("partial environment unexpectedly took the legacy error path")
	}

	if _, err := io.WriteString(terminal, "\x03"); err != nil {
		t.Fatal(err)
	}

	if err := command.Wait(); err == nil {
		t.Fatal("cancelled first-run setup unexpectedly succeeded")
	}
}

func TestConfiguredNonSandboxProfileCanBeSelectedFromTerminal(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "test-binary")

	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v %s", err, output)
	}

	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)

	profiles, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}

	profile := config.Profile{Name: "local-test", Environment: "simulator", Account: "test-account", SecretFile: true}
	if err := profiles.Save(profile, "profile-secret-sentinel"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, binary)

	command.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+root, "WOOVI_API_BASE_URL=", "WOOVI_APP_ID=")

	terminal, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close() }()

	output := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 1024)
		for {
			count, readError := terminal.Read(buffer)
			if count > 0 {
				output <- append([]byte(nil), buffer[:count]...)
			}

			if readError != nil {
				close(output)
				return
			}
		}
	}()

	var transcript strings.Builder

	waitFor := func(expected string) {
		t.Helper()

		for !strings.Contains(transcript.String(), expected) {
			select {
			case data, open := <-output:
				if !open {
					t.Fatalf("terminal ended before %q: %s", expected, transcript.String())
				}

				transcript.Write(data)
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %q: %s", expected, transcript.String())
			}
		}
	}

	waitFor("❯ local-test")

	if _, err := io.WriteString(terminal, "\r"); err != nil {
		t.Fatal(err)
	}

	waitFor("starting stdio MCP server")

	if strings.Contains(transcript.String(), "profile-secret-sentinel") {
		t.Fatal("profile selection exposed the credential")
	}

	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	if err := command.Wait(); err == nil {
		t.Fatal("interrupted MCP server unexpectedly returned success")
	}
}

func TestInstallMCPSelectsAndRegistersProjectClientFromTerminal(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "test-binary")

	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v %s", err, output)
	}

	home := filepath.Join(root, "home")
	projectRoot := filepath.Join(root, "project")

	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	profiles, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}

	profile := config.Profile{Name: "terminal-test", Environment: "simulator", Account: "test-account", SecretFile: true}
	if err := profiles.Save(profile, "test-app-id"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, binary, "install-mcp", "--profile", "terminal-test", "--apply")
	command.Dir = projectRoot

	command.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home)

	terminal, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = terminal.Close() }()

	output := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 1024)
		for {
			count, readError := terminal.Read(buffer)
			if count > 0 {
				output <- append([]byte(nil), buffer[:count]...)
			}

			if readError != nil {
				close(output)
				return
			}
		}
	}()

	var transcript strings.Builder

	waitFor := func(expected string) {
		t.Helper()

		for !strings.Contains(transcript.String(), expected) {
			select {
			case data, open := <-output:
				if !open {
					t.Fatalf("terminal ended before %q: %s", expected, transcript.String())
				}

				transcript.Write(data)
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %q: %s", expected, transcript.String())
			}
		}
	}
	waitFor("Escolha o cliente MCP")

	if _, err := io.WriteString(terminal, "\x1b[B\x1b[B\x1b[B\x1b[B\r"); err != nil {
		t.Fatal(err)
	}

	waitFor("Cliente: gemini")
	waitFor("Registration saved")

	if err := command.Wait(); err != nil {
		t.Fatalf("install-mcp failed: %v; transcript: %s", err, transcript.String())
	}

	configPath := filepath.Join(projectRoot, ".gemini", "settings.json")

	installedConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(installedConfig), "woovi-pix-terminal-test") || !strings.Contains(string(installedConfig), "stdio") {
		t.Fatalf("Gemini project configuration missing registration: %s", installedConfig)
	}

	if strings.Contains(string(installedConfig), "test-app-id") {
		t.Fatal("MCP registration copied the credential into client configuration")
	}
}
