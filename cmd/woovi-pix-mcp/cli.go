package main

import (
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
		return defaultCommand(ctx, os.Getenv, in, out, logger, term.IsTerminal(int(os.Stdin.Fd())))
	}

	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(out, "Usage: woovi-pix-mcp [first-run setup or profile selection]\n       woovi-pix-mcp setup [--profile NAME] [--secret-file]\n       woovi-pix-mcp profiles\n       woovi-pix-mcp stdio --profile NAME\n       woovi-pix-mcp doctor --profile NAME\n       woovi-pix-mcp install-mcp [--profile NAME] [--client CLIENT] [--config PATH] [--apply]\n       woovi-pix-mcp install-agent-assets [--project PATH] [--apply]\n       woovi-pix-mcp hook session-start\n       woovi-pix-mcp replica-setup --profile NAME --replica-url URL [--endpoint URL] [--region REGION] [--secret-file]\n       woovi-pix-mcp replica-run|replica-status --profile NAME [--litestream PATH]\n       woovi-pix-mcp replica-restore --profile NAME --acknowledge-stale-state [--litestream PATH]\nSecrets are entered only in the trusted local terminal. setup does not contact Woovi.")
		return err
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	// Flag errors may echo unrecognized arguments (including pasted secrets).
	fs.SetOutput(io.Discard)
	name := fs.String("profile", "sandbox", "Saved profile name")
	fileSecret := fs.Bool("secret-file", false, "Explicitly store AppID in a restricted local file instead of the OS vault")
	client := fs.String("client", "", "Explicit client: opencode, claude, cursor, codex or gemini")
	clientPath := fs.String("config", "", "Explicit client configuration path")
	apply := fs.Bool("apply", false, "Apply registration with a private backup (default: preview)")
	project := fs.String("project", ".", "Project directory for agent skill and safety integrations")
	replicaURL := fs.String("replica-url", "", "Replica destination file:///absolute/path or s3://bucket/account-prefix")
	endpoint := fs.String("endpoint", "", "Optional S3-compatible endpoint (not credentials)")
	region := fs.String("region", "", "S3 region")
	litestream := fs.String("litestream", "litestream", "Litestream 0.5.x executable")

	ack := fs.Bool("acknowledge-stale-state", false, "Acknowledge restored idempotency history may be stale")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New("invalid command options; use --help (never pass secrets as options)")
	}

	if fs.NArg() != 0 && args[0] != "hook" {
		return errors.New("unexpected arguments; never pass AppID as an argument")
	}

	profiles, err := config.Default()
	if err != nil {
		return err
	}

	switch args[0] {
	case "hook":
		if fs.NArg() != 1 || fs.Arg(0) != "session-start" {
			return errors.New("expected hook session-start")
		}

		_, err := fmt.Fprintln(out, "Woovi Pix MCP safety: pix_create_charge may be enabled for this profile and creates a financial charge. Call it only when the user explicitly asks for charge creation. This reminder is guidance, not enforcement. Never expose or request the AppID.")

		return err
	case "install-agent-assets":
		binary, err := executableForClientConfig()
		if err != nil {
			return err
		}

		assets, err := config.InstallAgentAssets(*project, binary, *apply)
		if err != nil {
			return err
		}

		for _, asset := range assets {
			status := "already current"
			if asset.Change && *apply {
				status = "installed"
			} else if asset.Change {
				status = "preview"
			}

			if _, err := fmt.Fprintf(out, "%s: %s\n", status, asset.Path); err != nil {
				return err
			}
		}

		if !*apply {
			_, err = fmt.Fprintln(out, "Preview only; rerun with --apply to install project-scoped skills and safety reminders.")
		} else {
			_, err = fmt.Fprintln(out, "Installed project-scoped skill and safety integrations. Reload the OpenCode plugin or Claude Code skills if already running.")
		}

		return err
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
		selectedClient := *client
		if selectedClient == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return errors.New("select --client outside a terminal")
			}

			selectedClient, err = selectInstallClient(ctx, in, out)
			if err != nil {
				return err
			}
		}

		selectedPath := *clientPath
		if selectedPath == "" {
			selectedPath, err = defaultProjectClientConfig(selectedClient)
			if err != nil {
				return err
			}
		}

		if _, _, err := profiles.Load(*name); err != nil {
			return err
		}

		binary, err := executableForClientConfig()
		if err != nil {
			return err
		}

		path, err := filepath.Abs(selectedPath)
		if err != nil {
			return errors.New("invalid client configuration path")
		}

		if _, err := fmt.Fprintf(out, "Cliente: %s\nConfiguração do projeto: %s\n", selectedClient, path); err != nil {
			return err
		}

		changed, err := config.InstallMCP(path, selectedClient, binary, *name, *apply)
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
			return errors.New("setup requires an interactive terminal; run it directly in your terminal")
		}

		profile, secret, err := runSetupWizard(ctx, in, out, *name, *fileSecret, os.Getenv("WOOVI_API_BASE_URL"))
		if err != nil {
			return err
		}

		if err := profiles.Save(profile, secret); err != nil {
			return err
		}

		_, err = fmt.Fprintf(out, "Perfil salvo. Confira com `woovi-pix-mcp doctor --profile %s`. Para registrar o cliente: `woovi-pix-mcp install-mcp --profile %s` (preview; selecione o cliente e aplique com --apply). Instale o skill e avisos no projeto com `woovi-pix-mcp install-agent-assets` e depois `--apply`. Se estiver usando `go run`, compile um binário persistente antes de instalar integrações.\n", *name, *name)

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

func defaultCommand(ctx context.Context, getenv func(string) string, in io.Reader, out io.Writer, logger *log.Logger, interactive bool) error {
	if strings.TrimSpace(getenv("WOOVI_API_BASE_URL")) != "" && strings.TrimSpace(getenv("WOOVI_APP_ID")) != "" {
		return run(ctx, getenv, logger)
	}

	profiles, err := config.Default()
	if err != nil {
		return err
	}

	names, err := profiles.List()
	if err != nil {
		return err
	}

	if len(names) == 0 {
		if !interactive {
			return errors.New("woovi is not configured; run `woovi-pix-mcp setup --profile sandbox` in a terminal, then configure your MCP client with `stdio --profile sandbox`")
		}

		return cli(ctx, []string{"setup", "--profile", "sandbox"}, in, out, logger)
	}

	for _, name := range names {
		if name == "sandbox" {
			return runProfile(ctx, profiles, name, logger)
		}
	}

	if !interactive {
		return errors.New("select a configured profile with `woovi-pix-mcp stdio --profile NAME`; list profiles with `woovi-pix-mcp profiles`")
	}

	selectedProfile, err := selectConfiguredProfile(ctx, in, out, names)
	if err != nil {
		return err
	}

	return runProfile(ctx, profiles, selectedProfile, logger)
}

func runProfile(ctx context.Context, profiles config.Profiles, profileName string, logger *log.Logger) error {
	profile, secret, err := profiles.Load(profileName)
	if err != nil {
		return errors.New("selected profile or its credential is unavailable; check the OS credential vault or run doctor for that profile")
	}

	values := map[string]string{"WOOVI_API_BASE_URL": profile.BaseURL(), "WOOVI_APP_ID": secret, "WOOVI_ACCOUNT_ID": profile.Account, "WOOVI_ENABLE_CHARGE_CREATION": fmt.Sprint(profile.Writes), "WOOVI_DATABASE_PATH": profiles.DatabasePath(profile)}

	return run(ctx, func(key string) string { return values[key] }, logger)
}
