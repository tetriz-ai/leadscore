// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package leadscore_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Retained workflows use read-only permissions and omit artifact publication steps.
func TestWorkflowsDoNotPublishArtifacts(t *testing.T) {
	var files []string
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no reference workflows found")
	}
	pinned := regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var wf struct {
			On          map[string]any    `yaml:"on"`
			Permissions map[string]string `yaml:"permissions"`
			Jobs        map[string]struct {
				Permissions map[string]string `yaml:"permissions"`
				Steps       []struct {
					Uses string            `yaml:"uses"`
					Run  string            `yaml:"run"`
					With map[string]string `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &wf); err != nil {
			t.Fatal(err)
		}
		if filepath.Base(name) == "release.yml" {
			if _, ok := wf.On["workflow_dispatch"]; !ok || len(wf.On) != 1 {
				t.Errorf("%s must be manual validation only, got triggers %v", name, wf.On)
			}
		}
		checkPermissions := func(perms map[string]string) {
			for scope, level := range perms {
				if level != "read" && level != "none" {
					t.Errorf("%s grants %s: %s in a source-only workflow", name, scope, level)
				}
			}
		}
		checkPermissions(wf.Permissions)
		for _, job := range wf.Jobs {
			checkPermissions(job.Permissions)
			for _, step := range job.Steps {
				if step.Uses != "" && !pinned.MatchString(step.Uses) {
					t.Errorf("%s action %q is not SHA-pinned", name, step.Uses)
				}
				if strings.HasPrefix(step.Uses, "docker/build-push-action@") && step.With["push"] != "false" {
					t.Errorf("%s must build images without publishing them", name)
				}
				for _, publish := range []string{"gh release create", "gh release upload", "docker push", "--push", "npm publish"} {
					if strings.Contains(step.Run, publish) {
						t.Errorf("%s contains artifact publication command %q", name, publish)
					}
				}
			}
		}
	}
}
