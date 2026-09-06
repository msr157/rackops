package server

import (
	"fmt"
	"github.com/rackops/rackops/internal/types"
)

func publicService(s *types.Service) *types.Service {
	if s == nil {
		return nil
	}
	out := *s
	out.GitToken = ""
	out.Env = append([]types.EnvVar(nil), s.Env...)
	for i := range out.Env {
		out.Env[i].Value = ""
		out.Env[i].Redacted = true
	}
	return &out
}

// Centralize redaction for every service response, not just the list route.
func publicServiceResponse(v any) any {
	switch s := v.(type) {
	case *types.Service:
		return publicService(s)
	case types.Service:
		return publicService(&s)
	case []*types.Service:
		if s == nil {
			return s
		}
		out := make([]*types.Service, len(s))
		for i := range s {
			out[i] = publicService(s[i])
		}
		return out
	default:
		return v
	}
}

func resolveEnvUpdate(existing, update []types.EnvVar) ([]types.EnvVar, error) {
	old := map[string]string{}
	for _, v := range existing {
		old[v.Key] = v.Value
	}
	seen := map[string]bool{}
	out := append([]types.EnvVar(nil), update...)
	for i := range out {
		v := &out[i]
		if v.Key == "" || seen[v.Key] {
			return nil, fmt.Errorf("environment keys must be nonempty and unique")
		}
		seen[v.Key] = true
		if v.Redacted {
			value, ok := old[v.Key]
			if !ok || v.Value != "" {
				return nil, fmt.Errorf("supply a new value when adding or renaming an environment key")
			}
			v.Value = value
			v.Redacted = false
		}
	}
	return out, nil
}
