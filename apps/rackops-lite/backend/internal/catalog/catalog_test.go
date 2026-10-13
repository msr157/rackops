package catalog

import (
	"strings"
	"testing"
)

func TestAll_Integrity(t *testing.T) {
	templates := All()
	if len(templates) == 0 {
		t.Fatal("expected template catalog to be populated")
	}

	seenIDs := make(map[string]bool)
	validCategories := map[string]bool{
		"infra":    true,
		"homelab":  true,
		"ai":       true,
		"web":      true,
		"tools":    true,
		"games":    true,
		"database": true,
	}

	for _, tmpl := range templates {
		// Unique ID
		if tmpl.ID == "" {
			t.Errorf("template has empty ID: %+v", tmpl)
		}
		if seenIDs[tmpl.ID] {
			t.Errorf("duplicate template ID: %s", tmpl.ID)
		}
		seenIDs[tmpl.ID] = true

		// Required fields
		if tmpl.Name == "" {
			t.Errorf("template %s has empty Name", tmpl.ID)
		}
		if !validCategories[tmpl.Category] {
			t.Errorf("template %s has invalid category: %s", tmpl.ID, tmpl.Category)
		}
		if tmpl.Port <= 0 {
			t.Errorf("template %s has invalid port: %d", tmpl.ID, tmpl.Port)
		}
		if tmpl.Image == "" && tmpl.ComposeYML == "" {
			t.Errorf("template %s has neither Image nor ComposeYML", tmpl.ID)
		}
		if tmpl.MemMB <= 0 {
			t.Errorf("template %s has invalid MemMB: %d", tmpl.ID, tmpl.MemMB)
		}

		// Environment definitions
		for _, env := range tmpl.Env {
			if strings.TrimSpace(env.Key) == "" {
				t.Errorf("template %s has empty Env key", tmpl.ID)
			}
			if strings.TrimSpace(env.Label) == "" {
				t.Errorf("template %s env %s has empty Label", tmpl.ID, env.Key)
			}
		}
	}
}

func TestGet(t *testing.T) {
	minio := Get("minio")
	if minio == nil {
		t.Fatal("expected to find minio template")
	}
	if minio.Name != "MinIO" {
		t.Errorf("expected name MinIO, got %s", minio.Name)
	}

	nonExistent := Get("non-existent-template-id")
	if nonExistent != nil {
		t.Errorf("expected nil for non-existent template, got %+v", nonExistent)
	}
}
