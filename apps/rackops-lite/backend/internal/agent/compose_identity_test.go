package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rackops/rackops/internal/types"
)

func TestComposeIdentitiesAndLegacyOwnership(t *testing.T) {
	_, first, err := composeLocation(DeployPayload{Name: "same", ServiceID: "one"})
	if err != nil {
		t.Fatal(err)
	}
	dir, second, err := composeLocation(DeployPayload{Name: "same", ServiceID: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("same-name apps share a project")
	}
	_, legacy, err := composeLocation(DeployPayload{Name: "same", ServiceID: "two", ContainerID: "rackops-same"})
	if err != nil || legacy != "rackops-same" {
		t.Fatal("legacy volume namespace changed")
	}
	for _, workingDir := range []string{dir, filepath.Join(dir, "other"), ""} {
		err := verifyComposeOwnership(dir, legacy, func(args ...string) ([]byte, error) {
			if args[0] == "ps" {
				return []byte("container"), nil
			}
			return json.Marshal(map[string]string{"com.docker.compose.project": legacy, "com.docker.compose.project.working_dir": workingDir})
		})
		if (err == nil) != (workingDir == dir) {
			t.Fatalf("ownership %q: %v", workingDir, err)
		}
	}
}

func TestComposeRevisionPointerRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "active-revision"), []byte("revision-../../foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := composeArgs(dir, "project", "down"); err == nil {
		t.Fatal("accepted path escape")
	}
	if err := os.WriteFile(filepath.Join(dir, "active-revision"), []byte("revision-123"), 0600); err != nil {
		t.Fatal(err)
	}
	args, err := composeArgs(dir, "project", "logs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), filepath.Join(dir, "revision-123", "compose.yml")) {
		t.Fatal("selected wrong configuration revision")
	}
}

func TestComposeEnvironmentValidation(t *testing.T) {
	for _, env := range [][]types.EnvVar{
		{{Key: "BAD\nKEY", Value: "x"}}, {{Key: "COMPOSE_FILE", Value: "other.yml"}},
		{{Key: "A", Value: "x"}, {Key: "A", Value: "y"}}, {{Key: "A", Value: "bad\x00value"}}, {{Key: "A", Value: `a\'b`}},
	} {
		if _, err := composeEnvironment(env); err == nil {
			t.Fatal("accepted invalid environment")
		}
	}
	if data, err := composeEnvironment(nil); err != nil || len(data) != 0 {
		t.Fatal("removing all environment keys retained values")
	}
}

// This exercises the installed Compose parser without starting containers,
// contacting a daemon or pulling an image. CI must supply Docker Compose v2.
func TestComposeEnvironmentRoundtrip(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("Docker CLI not installed")
	}
	if err := exec.Command(docker, "compose", "version").Run(); err != nil {
		t.Skip("Compose v2 not installed")
	}
	for i, value := range []string{"", " spaces # $HOME ${SECRET} ", "it's private", `path\value`} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir := t.TempDir()
			env, err := composeEnvironment([]types.EnvVar{{Key: "RACKOPS_TEST_VALUE", Value: value}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".env"), env, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services:\n  app:\n    image: busybox:stable\n    environment:\n      TEST: ${RACKOPS_TEST_VALUE}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(docker, composeFilesArgs(dir, "rackops-test", dir, "config", "--format", "json")...)
			cmd.Dir = dir
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Services map[string]struct {
					Environment map[string]string `json:"environment"`
				} `json:"services"`
			}
			if err := json.Unmarshal(out, &config); err != nil {
				t.Fatal(err)
			}
			// Compose config escapes literal dollars for reusable output.
			if config.Services["app"].Environment["TEST"] != strings.ReplaceAll(value, "$", "$$") {
				t.Fatalf("Compose normalized environment: got %q want %q", config.Services["app"].Environment["TEST"], value)
			}
		})
	}
}
