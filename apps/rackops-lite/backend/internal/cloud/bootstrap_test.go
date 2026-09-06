package cloud

import (
	"context"
	"encoding/base64"
	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"os/exec"
	"strings"
	"testing"
)

func bootstrapFixture() BootstrapConfig {
	return BootstrapConfig{ServerURL: "https://control.example.test", OrgID: "org-fixture", Architecture: "amd64", BinaryURL: "https://github.com/msr157/rackops/releases/download/v0.1.0/rackops-linux-amd64", SHA256: strings.Repeat("a", 64)}
}
func TestBootstrapPinsArtifactAndKeepsTokenOutOfProcessArguments(t *testing.T) {
	token := "rkenroll_" + strings.Repeat("b", 64)
	script, err := RenderBootstrap(bootstrapFixture(), token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(script, token) != 1 || strings.Contains(script, "--token "+token) || strings.Contains(script, "set -x") || !strings.Contains(script, "sha256sum --check --status") || !strings.Contains(script, "--token-file") {
		t.Fatal("unsafe bootstrap payload")
	}
	if strings.Index(script, "sha256sum --check") > strings.Index(script, "install -m 0755") {
		t.Fatal("binary installed before verification")
	}
	bad := bootstrapFixture()
	bad.ServerURL = "https://example.test/$(id)"
	if _, err := RenderBootstrap(bad, token); err == nil {
		t.Fatal("shell syntax accepted in URL")
	}
	bad = bootstrapFixture()
	bad.BinaryURL = "https://github.com/msr157/rackops/releases/latest/download/rackops-linux-amd64"
	if _, err := RenderBootstrap(bad, token); err == nil {
		t.Fatal("mutable release accepted")
	}
	bad = bootstrapFixture()
	bad.Architecture = "arm64"
	if _, err := RenderBootstrap(bad, token); err == nil {
		t.Fatal("architecture mismatch accepted")
	}
}
func TestBootstrapAdapterCopySuppliesAWSUserData(t *testing.T) {
	script, _ := RenderBootstrap(bootstrapFixture(), "rkenroll_"+strings.Repeat("b", 64))
	f := &fakeEC2{instances: []ec2types.Instance{{InstanceId: aws.String("i-fixture1")}}}
	original := &AWS{compute: f}
	p, err := WithBootstrap(original, script)
	if err != nil {
		t.Fatal(err)
	}
	spec := fixtureSpec()
	if _, err := p.Start(context.Background(), MachineSpec{AWS: &spec}); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(aws.ToString(f.requests[0].UserData))
	if err != nil || string(decoded) != script || original.bootstrap != "" {
		t.Fatal("bootstrap payload lost or shared with another operation")
	}
	f.err = &smithy.GenericAPIError{Code: "DryRunOperation"}
	if err := p.(*AWS).CheckLaunchPermission(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 2 || aws.ToString(f.requests[1].UserData) != aws.ToString(f.requests[0].UserData) || !aws.ToBool(f.requests[1].DryRun) {
		t.Fatal("permission check did not inspect the same bootstrap payload")
	}
}

func TestBootstrapShellSyntax(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable; exercised on Linux")
	}
	script, err := RenderBootstrap(bootstrapFixture(), "rkenroll_"+strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(shell, "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap syntax: %v %s", err, output)
	}
}
