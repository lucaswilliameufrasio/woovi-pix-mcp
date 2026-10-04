package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/config"
	"golang.org/x/term"
)

func cli(ctx context.Context, args []string, in io.Reader, out io.Writer, logger *log.Logger) error {
	if len(args) == 0 {
		return run(ctx, os.Getenv, logger)
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(out, "Usage: woovi-pix-mcp setup [--profile NAME] [--secret-file]\n       woovi-pix-mcp stdio --profile NAME\n       woovi-pix-mcp doctor --profile NAME\nSecrets are entered only in the trusted local terminal. setup does not contact Woovi.")
		return err
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(out)
	name := fs.String("profile", "sandbox", "Saved profile name")
	fileSecret := fs.Bool("secret-file", false, "Explicitly store AppID in a restricted local file instead of the OS vault")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments; never pass AppID as an argument")
	}
	profiles, err := config.Default()
	if err != nil {
		return err
	}
	switch args[0] {
	case "setup":
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("setup requires an interactive terminal for secret entry")
		}
		reader := bufio.NewReader(in)
		prompt := func(label string) (string, error) {
			if _, err := fmt.Fprint(out, label); err != nil {
				return "", err
			}
			line, err := reader.ReadString('\n')
			return strings.TrimSpace(line), err
		}
		env, err := prompt("Environment (sandbox/production/simulator): ")
		if err != nil {
			return err
		}
		account, err := prompt("Account identifier (stable local scope, not a secret): ")
		if err != nil {
			return err
		}
		write, err := prompt("Enable charge creation? Type yes to opt in: ")
		if err != nil {
			return err
		}
		p := config.Profile{Name: *name, Environment: env, Account: account, Writes: write == "yes", SecretFile: *fileSecret}
		if err := p.Validate(); err != nil {
			return err
		}
		if *fileSecret {
			_, _ = fmt.Fprintln(out, "AppID will be stored unencrypted in a private 0600 file. Do not share/back up it unintentionally.")
		}
		_, _ = fmt.Fprint(out, "AppID (from your Woovi environment; input hidden): ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(out)
		if err != nil {
			return errors.New("unable to read hidden AppID")
		}
		if err := profiles.Save(p, strings.TrimSpace(string(secret))); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Profile saved. Run doctor --profile", *name, "then configure your MCP client with stdio --profile", *name)
		return err
	case "stdio", "doctor":
		p, secret, err := profiles.Load(*name)
		if err != nil {
			return err
		}
		if args[0] == "doctor" {
			_, err := fmt.Fprintf(out, "Profile: %s\nEnvironment: %s\nCredential: available (hidden)\nCharge creation: %t\nSQLite: local, automatic\nNo provider request was made.\n", p.Name, p.Environment, p.Writes)
			return err
		}
		values := map[string]string{"WOOVI_API_BASE_URL": p.BaseURL(), "WOOVI_APP_ID": secret, "WOOVI_ACCOUNT_ID": p.Account, "WOOVI_ENABLE_CHARGE_CREATION": fmt.Sprint(p.Writes), "WOOVI_DATABASE_PATH": profiles.DatabasePath(p)}
		return run(ctx, func(key string) string { return values[key] }, logger)
	default:
		return errors.New("unknown command; run woovi-pix-mcp --help")
	}
}
