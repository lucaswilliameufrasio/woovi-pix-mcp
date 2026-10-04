package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflow struct {
	Events      map[string]any         `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs       any               `yaml:"needs"`
	Uses        string            `yaml:"uses"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowStep struct {
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
}

type releaseConfig struct {
	Builds  []releaseBuild `yaml:"builds"`
	Release struct {
		Draft bool `yaml:"draft"`
	} `yaml:"release"`
	Checksum struct {
		Algorithm string `yaml:"algorithm"`
	} `yaml:"checksum"`
}

type releaseBuild struct {
	Systems       []string `yaml:"goos"`
	Architectures []string `yaml:"goarch"`
	Environment   []string `yaml:"env"`
	LinkerFlags   []string `yaml:"ldflags"`
}

func loadYAML(t *testing.T, path string, destination any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := yaml.Unmarshal(data, destination); err != nil {
		t.Fatal(err)
	}
}

func loadWorkflow(t *testing.T, name string) workflow {
	t.Helper()

	var configuration workflow
	loadYAML(t, filepath.Join("..", ".github", "workflows", name), &configuration)

	return configuration
}

func TestReleaseActionsAreImmutable(t *testing.T) {
	immutableReference := regexp.MustCompile(`@[0-9a-f]{40}$`)

	for _, name := range []string{"ci.yml", "prepare-release.yml", "release.yml"} {
		t.Run(name, func(t *testing.T) {
			configuration := loadWorkflow(t, name)

			for jobName, job := range configuration.Jobs {
				for _, step := range job.Steps {
					if step.Uses != "" && !immutableReference.MatchString(step.Uses) {
						t.Fatalf("%s action is not pinned: %s", jobName, step.Uses)
					}
				}
			}
		})
	}
}

func TestReleasePermissionsDefaultToReadOnly(t *testing.T) {
	for _, name := range []string{"ci.yml", "prepare-release.yml", "release.yml"} {
		configuration := loadWorkflow(t, name)
		if configuration.Permissions["contents"] != "read" {
			t.Fatalf("%s must default to read", name)
		}
	}
}

func TestCISupportsExplicitAndReusableInvocation(t *testing.T) {
	configuration := loadWorkflow(t, "ci.yml")

	for _, event := range []string{"workflow_call", "workflow_dispatch", "pull_request", "push"} {
		if _, ok := configuration.Events[event]; !ok {
			t.Fatalf("CI missing event %s", event)
		}
	}
}

func TestReleaseWaitsForValidationAndQuality(t *testing.T) {
	configuration := loadWorkflow(t, "release.yml")
	if configuration.Jobs["quality"].Uses != "./.github/workflows/ci.yml" {
		t.Fatal("release must reuse the full CI gate")
	}

	dependencies, ok := configuration.Jobs["release"].Needs.([]any)
	if !ok || !slices.Equal(dependencies, []any{"validate", "quality"}) {
		t.Fatal("release publication must wait for validation and quality")
	}
}

func TestPreparationDispatchesCIWithoutTagsOrForcePush(t *testing.T) {
	configuration := loadWorkflow(t, "prepare-release.yml")

	var commands strings.Builder

	for _, step := range configuration.Jobs["prepare"].Steps {
		commands.WriteString(step.Run)
	}

	script := commands.String()
	if !strings.Contains(script, `gh workflow run ci.yml --ref "$branch"`) {
		t.Fatal("bot-created PR needs explicit CI dispatch")
	}

	if strings.Contains(script, "--force") || strings.Contains(script, "git tag ") {
		t.Fatal("preparation must not force push or create tags")
	}
}

func TestReleaseIsADraftWithSHA256Checksums(t *testing.T) {
	var configuration releaseConfig
	loadYAML(t, filepath.Join("..", ".goreleaser.yaml"), &configuration)

	if !configuration.Release.Draft || configuration.Checksum.Algorithm != "sha256" {
		t.Fatal("release must be a draft with SHA-256 checksums")
	}
}

func TestReleaseBuildsSixPureGoTargetsWithVersionMetadata(t *testing.T) {
	var configuration releaseConfig
	loadYAML(t, filepath.Join("..", ".goreleaser.yaml"), &configuration)

	if len(configuration.Builds) != 1 {
		t.Fatal("only the MCP binary should be shipped")
	}

	build := configuration.Builds[0]
	if !slices.Equal(build.Systems, []string{"linux", "darwin", "windows"}) ||
		!slices.Equal(build.Architectures, []string{"amd64", "arm64"}) ||
		!slices.Equal(build.Environment, []string{"CGO_ENABLED=0"}) {
		t.Fatal("six pure-Go targets required, without provider credentials")
	}

	flags := strings.Join(build.LinkerFlags, " ")
	if !strings.Contains(flags, "internal/buildinfo.Version=") ||
		!strings.Contains(flags, "internal/buildinfo.Commit=") {
		t.Fatal("version/commit injection missing")
	}
}
