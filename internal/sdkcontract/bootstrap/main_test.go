package main

import (
	"os/exec"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBootstrapEmitsReferencedSchemas(t *testing.T) {
	data, err := exec.Command("go", "run", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	var schemas map[string]any
	if err := yaml.Unmarshal(data, &schemas); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"BrainEntry", "CreateEntryRequest", "ResolvedTask", "AttachmentReference"} {
		if schemas[name] == nil {
			t.Fatalf("missing %s", name)
		}
	}
}
