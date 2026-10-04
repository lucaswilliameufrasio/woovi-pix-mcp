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
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/buildinfo"
	"github.com/lucaseufrasio/woovi-pix-mcp/internal/config"
)

func cli(ctx context.Context, args []string, in io.Reader, out io.Writer, logger *log.Logger) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		_, err := fmt.Fprintf(out, "woovi-pix-mcp %s (commit %s)\n", buildinfo.Version, buildinfo.Commit)
		return err
	}

	if len(args) == 0 {
		return run(ctx, os.Getenv, logger)
	}

	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(out, "Usage: woovi-pix-mcp setup [--profile NAME] [--secret-file]\n       woovi-pix-mcp stdio --profile NAME\n       woovi-pix-mcp doctor --profile NAME\n       woovi-pix-mcp install-mcp --profile NAME --client CLIENT --config PATH [--apply]\n       woovi-pix-mcp replica-setup --profile NAME --replica-url URL [--endpoint URL] [--region REGION] [--secret-file]\n       woovi-pix-mcp replica-run|replica-status --profile NAME [--litestream PATH]\n       woovi-pix-mcp replica-restore --profile NAME --acknowledge-stale-state [--litestream PATH]\nSecrets are entered only in the trusted local terminal. setup does not contact Woovi.")
		return err
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	// Flag errors may echo unrecognized arguments (including pasted secrets).
	fs.SetOutput(io.Discard)
	name := fs.String("profile", "sandbox", "Saved profile name")
	fileSecret := fs.Bool("secret-file", false, "Explicitly store AppID in a restricted local file instead of the OS vault")
	client := fs.String("client", "", "Explicit client: claude, cursor or opencode (V2)")
	clientPath := fs.String("config", "", "Explicit client JSON configuration path")
	apply := fs.Bool("apply", false, "Apply registration with a private backup (default: preview)")
	replicaURL := fs.String("replica-url", "", "Replica destination file:///absolute/path or s3://bucket/account-prefix")
	endpoint := fs.String("endpoint", "", "Optional S3-compatible endpoint (not credentials)")
	region := fs.String("region", "", "S3 region")
	litestream := fs.String("litestream", "litestream", "Litestream 0.5.x executable")

	ack := fs.Bool("acknowledge-stale-state", false, "Acknowledge restored idempotency history may be stale")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New("invalid command options; use --help (never pass secrets as options)")
	}

	if fs.NArg() != 0 {
		return errors.New("unexpected arguments; never pass AppID as an argument")
	}

	profiles, err := config.Default()
	if err != nil {
		return err
	}

	switch args[0] {
	case "profiles":
		names, err := profiles.List()
		if err != nil {
			return err
		}

		if len(names) == 0 {
			_, err = fmt.Fprintln(out, "No profiles configured; run setup.")
			return err
		}

		for _, name := range names {
			if _, err := fmt.Fprintln(out, name); err != nil {
				return err
			}
		}

		return nil
	case "replica-setup", "replica-run", "replica-status", "replica-restore":
		p, _, err := profiles.Load(*name)
		if err != nil {
			return err
		}

		if args[0] == "replica-setup" {
			r := config.Replica{URL: *replicaURL, Endpoint: *endpoint, Region: *region, SecretFile: *fileSecret}
			if err := r.Validate(); err != nil {
				return err
			}

			var c config.ReplicaCredential

			if strings.HasPrefix(r.URL, "s3:") {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return errors.New("S3 credentials require an interactive terminal")
				}

				if *fileSecret {
					_, _ = fmt.Fprintln(out, "Replica credentials will be saved unencrypted in a private file.")
				}

				_, _ = fmt.Fprint(out, "S3 access key (hidden): ")
				key, err := term.ReadPassword(int(os.Stdin.Fd()))
				_, _ = fmt.Fprintln(out)

				if err != nil {
					return errors.New("unable to read hidden access key")
				}

				_, _ = fmt.Fprint(out, "S3 secret key (hidden): ")
				secret, err := term.ReadPassword(int(os.Stdin.Fd()))
				_, _ = fmt.Fprintln(out)

				if err != nil {
					return errors.New("unable to read hidden secret key")
				}

				c = config.ReplicaCredential{AccessKey: strings.TrimSpace(string(key)), SecretKey: strings.TrimSpace(string(secret))}
			}

			if err := profiles.SaveReplica(p, r, c); err != nil {
				return err
			}

			_, err = fmt.Fprintln(out, "Replica configuration saved. Start replica-run separately; MCP does not depend on it.")

			return err
		}

		action := map[string]string{"replica-run": "replicate", "replica-status": "status", "replica-restore": "restore"}[args[0]]
		if args[0] == "replica-run" {
			_, _ = fmt.Fprintln(out, "Replication starting in foreground; stop with Ctrl+C. No automatic HA or multiwriter.")
		}

		if err := profiles.RunReplica(ctx, p, *litestream, action, *ack); err != nil {
			return err
		}

		_, err = fmt.Fprintln(out, "Litestream operation succeeded. This does not guarantee zero replica lag.")

		return err
	case "install-mcp":
		if *clientPath == "" {
			return errors.New("select --client and --config explicitly")
		}

		if _, _, err := profiles.Load(*name); err != nil {
			return err
		}

		binary, err := os.Executable()
		if err != nil {
			return errors.New("unable to locate executable")
		}

		path, err := filepath.Abs(*clientPath)
		if err != nil {
			return errors.New("invalid client configuration path")
		}

		changed, err := config.InstallMCP(path, *client, binary, *name, *apply)
		if err != nil {
			return err
		}

		if !changed {
			_, err = fmt.Fprintln(out, "Registration already matches; no changes.")
		} else if *apply {
			_, err = fmt.Fprintln(out, "Registration saved; existing configuration backed up. No credential copied.")
		} else {
			_, err = fmt.Fprintln(out, "Registration preview validated; use --apply to save. No files changed.")
		}

		return err
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
			state, err := profiles.Diagnose(p)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(out, "Profile: %s\nEnvironment: %s\nCredential: available (hidden)\nCharge creation configured: %t\nSQLite: %s\nNo provider request was made.\n", p.Name, p.Environment, p.Writes, state)

			return err
		}

		values := map[string]string{"WOOVI_API_BASE_URL": p.BaseURL(), "WOOVI_APP_ID": secret, "WOOVI_ACCOUNT_ID": p.Account, "WOOVI_ENABLE_CHARGE_CREATION": fmt.Sprint(p.Writes), "WOOVI_DATABASE_PATH": profiles.DatabasePath(p)}

		return run(ctx, func(key string) string { return values[key] }, logger)
	default:
		return errors.New("unknown command; run woovi-pix-mcp --help")
	}
}
