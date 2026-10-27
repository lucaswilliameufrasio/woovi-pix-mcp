package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/config"
)

type setupStep int

const (
	setupEnvironmentStep setupStep = iota
	setupAccountStep
	setupWritesStep
	setupAppIDStep
)

type setupEnvironment struct {
	name  string
	label string
	url   string
}

type setupWizardModel struct {
	profileName    string
	secretFile     bool
	step           setupStep
	environment    int
	environments   []setupEnvironment
	accountInput   textinput.Model
	appIDInput     textinput.Model
	writesEnabled  bool
	completed      bool
	canceled       bool
	validationText string
}

type profileSelectorModel struct {
	profiles  []string
	selected  int
	confirmed bool
	canceled  bool
}

type installClientModel struct {
	clients   []string
	selected  int
	confirmed bool
	canceled  bool
}

func selectInstallClient(ctx context.Context, in io.Reader, out io.Writer) (string, error) {
	model := installClientModel{clients: []string{"opencode", "claude", "cursor", "codex", "gemini"}}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))

	result, err := program.Run()
	if err != nil {
		return "", errors.New("client selector could not run; use it from an interactive terminal")
	}

	finalModel, ok := result.(installClientModel)
	if !ok {
		return "", errors.New("client selector returned an invalid result")
	}

	if finalModel.canceled || !finalModel.confirmed {
		return "", errors.New("client selection canceled")
	}

	return finalModel.clients[finalModel.selected], nil
}

func (model installClientModel) Init() tea.Cmd {
	return nil
}

func (model installClientModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	keyMessage, ok := message.(tea.KeyPressMsg)
	if !ok {
		return model, nil
	}

	switch keyMessage.String() {
	case "ctrl+c", "esc":
		model.canceled = true
		return model, tea.Quit
	case "up", "k":
		model.selected = (model.selected + len(model.clients) - 1) % len(model.clients)
	case "down", "j":
		model.selected = (model.selected + 1) % len(model.clients)
	case "enter":
		model.confirmed = true
		return model, tea.Quit
	}

	return model, nil
}

func (model installClientModel) View() tea.View {
	var view strings.Builder
	view.WriteString("Escolha o cliente MCP (↑/↓, Enter)\n\n")

	for index, client := range model.clients {
		marker := "  "
		if index == model.selected {
			marker = "❯ "
		}

		view.WriteString(marker + client + "\n")
	}

	view.WriteString("\nEsc para cancelar")

	return tea.NewView(view.String())
}

func defaultProjectClientConfig(client string) (string, error) {
	switch client {
	case "opencode":
		return filepath.Join("opencode.json"), nil
	case "claude":
		return filepath.Join(".mcp.json"), nil
	case "cursor":
		return filepath.Join(".cursor", "mcp.json"), nil
	case "codex":
		return filepath.Join(".codex", "config.toml"), nil
	case "gemini":
		return filepath.Join(".gemini", "settings.json"), nil
	default:
		return "", errors.New("unsupported MCP client")
	}
}

func selectConfiguredProfile(ctx context.Context, in io.Reader, out io.Writer, profileNames []string) (string, error) {
	model := profileSelectorModel{profiles: profileNames}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))

	result, err := program.Run()
	if err != nil {
		return "", errors.New("profile selector could not run; use it from an interactive terminal")
	}

	finalModel, ok := result.(profileSelectorModel)
	if !ok {
		return "", errors.New("profile selector returned an invalid result")
	}

	if finalModel.canceled || !finalModel.confirmed {
		return "", errors.New("profile selection canceled")
	}

	return finalModel.profiles[finalModel.selected], nil
}

func (model profileSelectorModel) Init() tea.Cmd {
	return nil
}

func (model profileSelectorModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	keyMessage, ok := message.(tea.KeyPressMsg)
	if !ok {
		return model, nil
	}

	switch keyMessage.String() {
	case "ctrl+c", "esc":
		model.canceled = true
		return model, tea.Quit
	case "up", "k":
		model.selected = (model.selected + len(model.profiles) - 1) % len(model.profiles)
	case "down", "j":
		model.selected = (model.selected + 1) % len(model.profiles)
	case "enter":
		model.confirmed = true
		return model, tea.Quit
	}

	return model, nil
}

func (model profileSelectorModel) View() tea.View {
	var view strings.Builder
	view.WriteString("Woovi Pix MCP — escolha o perfil (↑/↓, Enter)\n\n")

	for index, profileName := range model.profiles {
		marker := "  "
		if index == model.selected {
			marker = "❯ "
		}

		view.WriteString(marker + profileName + "\n")
	}

	view.WriteString("\nEsc para cancelar")

	return tea.NewView(view.String())
}

func runSetupWizard(ctx context.Context, in io.Reader, out io.Writer, profileName string, secretFile bool, suggestedBaseURL string) (config.Profile, string, error) {
	model := newSetupWizardModel(profileName, secretFile, suggestedBaseURL)
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))

	result, err := program.Run()
	if err != nil {
		return config.Profile{}, "", errors.New("setup wizard could not run; use it from an interactive terminal")
	}

	finalModel, ok := result.(setupWizardModel)
	if !ok {
		return config.Profile{}, "", errors.New("setup wizard returned an invalid result")
	}

	if finalModel.canceled {
		return config.Profile{}, "", errors.New("setup canceled; no profile was saved")
	}

	if !finalModel.completed {
		return config.Profile{}, "", errors.New("setup did not complete; no profile was saved")
	}

	profile := config.Profile{
		Name:        finalModel.profileName,
		Environment: finalModel.environments[finalModel.environment].name,
		Account:     strings.TrimSpace(finalModel.accountInput.Value()),
		Writes:      finalModel.writesEnabled,
		SecretFile:  finalModel.secretFile,
	}
	if err := profile.Validate(); err != nil {
		return config.Profile{}, "", err
	}

	return profile, strings.TrimSpace(finalModel.appIDInput.Value()), nil
}

func newSetupWizardModel(profileName string, secretFile bool, suggestedBaseURL string) setupWizardModel {
	accountInput := textinput.New()
	accountInput.Prompt = "› "
	accountInput.Placeholder = "Ex.: minha-conta-sandbox"
	accountInput.CharLimit = 256
	accountInput.SetWidth(60)

	appIDInput := textinput.New()
	appIDInput.Prompt = "› "
	appIDInput.Placeholder = "AppID Woovi"
	appIDInput.EchoMode = textinput.EchoPassword
	appIDInput.EchoCharacter = '•'
	appIDInput.CharLimit = 1024
	appIDInput.SetWidth(60)

	model := setupWizardModel{
		profileName: profileName,
		secretFile:  secretFile,
		environments: []setupEnvironment{
			{name: "sandbox", label: "Sandbox", url: config.Profile{Environment: "sandbox"}.BaseURL()},
			{name: "production", label: "Produção", url: config.Profile{Environment: "production"}.BaseURL()},
			{name: "simulator", label: "Simulador local", url: config.Profile{Environment: "simulator"}.BaseURL()},
		},
		accountInput: accountInput,
		appIDInput:   appIDInput,
	}

	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(suggestedBaseURL), "/")
	for index, environment := range model.environments {
		if environment.url == trimmedBaseURL {
			model.environment = index
			break
		}
	}

	return model
}

func (model setupWizardModel) Init() tea.Cmd {
	return nil
}

func (model setupWizardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	keyMessage, ok := message.(tea.KeyPressMsg)
	if !ok {
		return model, nil
	}

	key := keyMessage.String()
	if key == "ctrl+c" || key == "esc" {
		model.canceled = true
		return model, tea.Quit
	}

	switch model.step {
	case setupEnvironmentStep:
		switch key {
		case "up", "k":
			model.environment = (model.environment + len(model.environments) - 1) % len(model.environments)
		case "down", "j":
			model.environment = (model.environment + 1) % len(model.environments)
		case "enter":
			model.step = setupAccountStep
			model.validationText = ""

			return model, model.accountInput.Focus()
		}
	case setupAccountStep:
		if key == "enter" {
			if strings.TrimSpace(model.accountInput.Value()) == "" {
				model.validationText = "Informe um identificador estável para esta conta."
				return model, nil
			}

			model.step = setupWritesStep
			model.validationText = ""
			model.accountInput.Blur()

			return model, nil
		}

		var command tea.Cmd

		model.accountInput, command = model.accountInput.Update(message)

		return model, command
	case setupWritesStep:
		switch key {
		case "up", "left", "n":
			model.writesEnabled = false
		case "down", "right", "y":
			model.writesEnabled = true
		case "enter":
			model.step = setupAppIDStep
			if model.secretFile {
				model.validationText = "Atenção: o AppID ficará sem criptografia em arquivo privado."
			} else {
				model.validationText = "O AppID será salvo no cofre de credenciais do sistema."
			}

			return model, model.appIDInput.Focus()
		}
	case setupAppIDStep:
		if key == "enter" {
			if strings.TrimSpace(model.appIDInput.Value()) == "" {
				model.validationText = "Informe o AppID; a entrada fica oculta."
				return model, nil
			}

			model.completed = true

			return model, tea.Quit
		}

		var command tea.Cmd

		model.appIDInput, command = model.appIDInput.Update(message)

		return model, command
	}

	return model, nil
}

func (model setupWizardModel) View() tea.View {
	var view strings.Builder
	view.WriteString("Woovi Pix MCP — configuração inicial\n\n")

	switch model.step {
	case setupEnvironmentStep:
		view.WriteString("Escolha o ambiente (↑/↓, Enter):\n")

		for index, environment := range model.environments {
			marker := "  "
			if index == model.environment {
				marker = "❯ "
			}

			_, _ = fmt.Fprintf(&view, "%s%-12s %s\n", marker, environment.label, environment.url)
		}
	case setupAccountStep:
		view.WriteString("Identificador estável da conta (não é segredo):\n")
		view.WriteString(model.accountInput.View())
		view.WriteString("\n\nEnter para continuar")
	case setupWritesStep:
		view.WriteString("Permitir criação de cobranças Pix? (←/→, Enter)\n")

		readOnlyMarker := "  "
		writeMarker := "  "

		if !model.writesEnabled {
			readOnlyMarker = "❯ "
		} else {
			writeMarker = "❯ "
		}

		view.WriteString(readOnlyMarker + "Somente consulta (recomendado)\n")
		view.WriteString(writeMarker + "Sim, habilitar criação\n")
		view.WriteString("\nCriação é opt-in e pode iniciar operações financeiras.\n")
	case setupAppIDStep:
		view.WriteString("AppID da conta selecionada (entrada oculta):\n")
		view.WriteString(model.appIDInput.View())
		view.WriteString("\n\nEnter para salvar; Esc para cancelar")
	}

	if model.validationText != "" {
		view.WriteString("\n\n")
		view.WriteString(model.validationText)
	}

	return tea.NewView(view.String())
}
