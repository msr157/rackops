package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReplacementRetainsPreviousAndDisablesRestart(t *testing.T) {
	p := DeployPayload{ServiceID: "service", Name: "app"}
	var commands []string
	name, err := retainPrevious(p, func(args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "ps":
			return []byte("existing|" + containerName(p)), nil
		case "inspect":
			return []byte(`{"rackops.service":"service"}`), nil
		case "update", "stop", "rename":
			return nil, nil
		default:
			t.Fatalf("unexpected command %v", args)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !retainedContainer(name) {
		t.Fatalf("retained container would appear active: %s", name)
	}
	if len(commands) != 5 || commands[2] != "update --restart=no existing" || commands[3] != "stop --time 30 existing" || commands[4] != "rename existing "+name {
		t.Fatalf("unsafe replacement order: %v", commands)
	}
}

func TestReplacementRefusesUnverifiedOwnershipAndReportsInterruption(t *testing.T) {
	p := DeployPayload{ServiceID: "service", Name: "app"}
	for _, failure := range []string{"ps", "ownership", "update", "stop", "rename"} {
		t.Run(failure, func(t *testing.T) {
			_, err := retainPrevious(p, func(args ...string) ([]byte, error) {
				if args[0] == failure {
					return nil, fmt.Errorf("injected failure")
				}
				switch args[0] {
				case "ps":
					return []byte("existing|" + containerName(p)), nil
				case "inspect":
					if failure == "ownership" {
						return []byte(`{"rackops.service":"someone-else"}`), nil
					}
					return []byte(`{"rackops.service":"service"}`), nil
				case "update", "stop", "rename":
					if failure == "ownership" {
						t.Fatal("mutated foreign container")
					}
					return nil, nil
				default:
					t.Fatalf("unexpected command %v", args)
					return nil, nil
				}
			})
			if err == nil {
				t.Fatal("failure was ignored")
			}
			var recovery *recoveryError
			if want := failure == "update" || failure == "stop" || failure == "rename"; errors.As(err, &recovery) != want {
				t.Fatalf("incorrect recovery state: %v", err)
			}
		})
	}
}

func TestReplacementHealthFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		state           string
		healthy, failed bool
	}{
		{`{"Status":"running"}`, true, false},
		{`{"Status":"running","Health":{"Status":"healthy"}}`, true, false},
		{`{"Status":"running","Health":{"Status":"starting"}}`, false, false},
		{`{"Status":"running","Health":{"Status":"unhealthy"}}`, false, true},
		{`{"Status":"exited"}`, false, true},
		{`{}`, false, true},
		{`invalid`, false, true},
	} {
		healthy, err := containerHealth([]byte(tc.state))
		if healthy != tc.healthy || (err != nil) != tc.failed {
			t.Fatalf("health %s: %v %v", tc.state, healthy, err)
		}
	}
}
