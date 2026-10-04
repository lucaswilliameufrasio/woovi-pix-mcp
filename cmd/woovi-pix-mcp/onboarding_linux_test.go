//go:build linux

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
	"golang.org/x/sys/unix"

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

	cmd := exec.CommandContext(ctx, binary, "setup", "--profile", "tty-test", "--secret-file")

	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+root)

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
				t.Fatal("terminal prompt timed out")
			}
		}
	}
	for _, step := range []struct{ prompt, answer string }{{"Environment", "sandbox"}, {"Account identifier", "test-account"}, {"Enable charge creation", "no"}, {"input hidden", "tty-secret-sentinel"}} {
		waitFor(step.prompt)

		if step.prompt == "input hidden" {
			// Printing the prompt and ReadPassword disabling ECHO are separate
			// syscalls. Wait for the actual terminal state, not an arbitrary
			// delay, before simulating entry into the hidden password prompt.
			for {
				state, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
				if err != nil {
					t.Fatal(err)
				}

				if state.Lflag&unix.ECHO == 0 {
					break
				}

				select {
				case <-ctx.Done():
					t.Fatal("terminal did not disable password echo")
				case <-time.After(time.Millisecond):
				}
			}
		}

		if _, err := io.WriteString(terminal, step.answer+"\n"); err != nil {
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

	s := config.Profiles{Root: filepath.Join(root, "woovi-pix-mcp")}

	_, secret, err := s.Load("tty-test")
	if err != nil || secret != "tty-secret-sentinel" {
		t.Fatal("profile did not persist hidden input")
	}

	command := exec.CommandContext(ctx, binary, "stdio", "--profile", "tty-test")
	// Deliberately poisoned environment values must not override a saved profile.
	command.Env = append(os.Environ(), "XDG_CONFIG_HOME="+root, "WOOVI_ENABLE_CHARGE_CREATION=true", "WOOVI_APP_ID=wrong")
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
