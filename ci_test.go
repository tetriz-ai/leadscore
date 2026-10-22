// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package leadscore_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The CI workflow's safety settings, held by a test so an edit cannot drop them
// silently.
func TestCIWorkflowSettings(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On struct {
			Schedule []map[string]string `yaml:"schedule"`
		} `yaml:"on"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress string `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	// Every push to main is its own group, so no main build is cancelled or dropped.
	if !strings.Contains(wf.Concurrency.Group, "github.sha") {
		t.Errorf("concurrency group %q must key push builds by commit", wf.Concurrency.Group)
	}
	if !strings.Contains(wf.Concurrency.CancelInProgress, "pull_request") {
		t.Errorf("cancel-in-progress %q must apply to pull requests only", wf.Concurrency.CancelInProgress)
	}
	var windowsSeen bool
	for _, job := range wf.Jobs {
		for _, st := range job.Steps {
			if strings.Contains(st.Run, "GOOS=windows go build") && strings.Contains(st.Run, "GOOS=windows go vet") {
				windowsSeen = true
			}
		}
	}
	if !windowsSeen {
		t.Error("CI must build and vet for Windows")
	}
	// The vulnerability scan runs weekly and on main, never on a pull request,
	// so a new advisory cannot fail a contributor's unrelated change.
	if len(wf.On.Schedule) == 0 {
		t.Error("CI must run on a weekly schedule")
	}
	if wf.Jobs["vulncheck"].If != "github.event_name != 'pull_request'" {
		t.Errorf("vulncheck must skip pull requests, has if %q", wf.Jobs["vulncheck"].If)
	}
	// Dependency updates are not automated: no Dependabot version-update config.
	for _, name := range []string{".github/dependabot.yml", ".github/dependabot.yaml"} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("%s must not exist", name)
		}
	}
}
