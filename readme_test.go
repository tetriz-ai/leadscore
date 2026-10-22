// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package leadscore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/tetriz-ai/leadscore/internal/config"
	"github.com/tetriz-ai/leadscore/internal/rules"
)

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// flatten joins every run of white space into one space, so a phrase matches
// however the prose is wrapped.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// section returns the text of doc from heading to the next heading of the
// same level or higher, skipping lines inside code blocks.
func section(t *testing.T, doc, heading string) string {
	t.Helper()
	i := strings.Index(doc, "\n"+heading+"\n")
	if i < 0 {
		t.Fatalf("no heading %q", heading)
	}
	rest := doc[i+len(heading)+2:]
	level := strings.Index(heading, " ")
	at, inCode := 0, false
	for _, line := range strings.SplitAfter(rest, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
		}
		n := strings.Index(line, " ")
		if !inCode && n > 0 && n <= level && strings.Trim(line[:n], "#") == "" {
			return rest[:at]
		}
		at += len(line)
	}
	return rest
}

// inOrder reports each step missing from text, or found before the step
// listed ahead of it.
func inOrder(t *testing.T, name, text string, steps []string) {
	t.Helper()
	at := 0
	for _, step := range steps {
		i := strings.Index(text[at:], step)
		if i < 0 {
			t.Errorf("%s has %q out of the runbook's order (or not at all)", name, step)
			continue
		}
		at += i
	}
}

// The README and the setup runbook (docs/setup.md) carry every duty the
// design gives them (sinks, receivers, the CLI, security and cost, export
// lists, the receiver, the setup runbooks and the vendor answers still
// unconfirmed), so a later edit cannot drop one unnoticed.
func TestReadmeDuties(t *testing.T) {
	readme := flatten(readFile(t, "README.md"))
	inReadme := map[string]string{
		"every path needs a terminal":                                 "Every path needs someone comfortable with a terminal",
		"no alerting in v1":                                           "There is no alerting in v1, on any path.",
		"quick start: drop do_not_contact rows before emailing":       "Before you email anyone on a list, drop rows where `do_not_contact` is `yes`.",
		"export opt-outs come only from receiver, polling, Overrides": "Opt-outs reach the lists from the receiver, polling and `Overrides`",
		"export-row limit per run":                                    "A run adds at most `ingest_chunk_rows` (2,000) new rows across all lists",
		"cost not measured yet":                                       "**Run time and monthly cost** on Google Cloud are not measured yet.",
		"event-rate advice":                                           "Above about 1,500 events a day, use SQLite",
		"receiver secret warning":                                     "works like a password: anyone who has it can send fake events",
		"one secret generator":                                        "Make a new secret with `openssl rand -hex 32`.",
		"Apollo-only opt-out warning":                                 "apollo-key:no_optout_flag",
		"CSV formula injection (csvsafe)":                             "never runs a formula a lead's data carried",
		"plug-in stores get no CSV files":                             "A plug-in store (for example Postgres, `docs/postgres-store.md`) keeps its lists as tables in that store.",
		"cold lanes claim leads with no sink":                         "A cold lane claims its leads even before its sink is set up",
		"Google Cloud: the Sheet holds personal data":                 "**The spreadsheet holds personal data.** Share it only with named people, never by link.",
		"uptime monitor":                                              "Set an uptime monitor on `<public_url>/healthz`.",
		"source-only release":                                         "Releases are source-only",
		"build the image yourself":                                    "docker build -t leadscore:local .",
		"the runbook link":                                            "[docs/setup.md](docs/setup.md)",
		"contributors":                                                "[CONTRIBUTORS.md](CONTRIBUTORS.md)",
		"an agent asks for the real leads":                            "An agent asks where your leads are before it copies or runs anything, never makes leads up",
	}
	for duty, phrase := range inReadme {
		if !strings.Contains(readme, flatten(phrase)) {
			t.Errorf("README lacks %s: %q", duty, phrase)
		}
	}
	// The command and receiver details the README links to.
	reference := flatten(readFile(t, "docs/reference.md"))
	for duty, phrase := range map[string]string{
		"receiver-silence needs public_url":            "Without `receiver.public_url`, silence is not watched",
		"the 503 cases of /healthz":                    "503 when the last run failed, none succeeded in three intervals, or the store cannot be read.",
		"dry-run ids are temporary on a first install": "On a first install its lead ids are temporary",
		"receiver-only push warning":                   "`receiver_only_push:<lead>`",
	} {
		if !strings.Contains(reference, flatten(phrase)) {
			t.Errorf("docs/reference.md lacks %s: %q", duty, phrase)
		}
	}

	setup := readFile(t, "docs/setup.md")
	flatSetup := flatten(setup)
	inSetup := map[string]string{
		"steps marked as agent or person":            "**[person]**",
		"no alerting in v1":                          "There is no alerting in v1, on any path.",
		"filter on do_not_contact":                   "Filter on `do_not_contact` before every send.",
		"vendor prerequisites per sink":              "**The HubSpot sink:** a HubSpot private app token.",
		"laptop: poll or tunnel":                     "A Cloudflare Tunnel",
		"laptop sleep warning":                       "A tunnel only works while the laptop is awake",
		"Workspace exception when sharing the Sheet": "ask your Workspace admin to allow sharing this file with the service accounts",
		"the Sheet holds personal data, at the step": "**The spreadsheet holds personal data (names, emails).** Share it only with named people, never by link.",
		"application-default login, with its undo":   "To undo it, run `gcloud auth application-default revoke`.",
		"owner edits protected tabs without warning": "edit the tabs leadscore protects (`Ranked`, `Health`, `Pushes`, ...) **with no warning**",
		"container CLI with the gcloud mount":        "-v ~/.config/gcloud:/home/leadscore/.config/gcloud:ro",
		"source installation":                        "Releases are source-only",
		"build the image yourself on Docker":         "docker build -t leadscore:local .",
		"plain-binary secret in this terminal only":  "export LEADSCORE_RECEIVER_SECRET=$(openssl rand -hex 32)",
		"the port in .env":                           "LEADSCORE_PORT=8080",
		"serve listens on every interface":           "`leadscore serve` listens on every network interface on `receiver.port`; the compose file publishes it on 127.0.0.1 only. Outside Docker, keep that port behind a firewall or a tunnel.",
		"upgrade and roll back":                      "Rolling back is the previous tag.",
		"dry-run summary line":                       "summary line starting `dry run`",
		"look leads up by email after a dry run":     "Look a lead up by email with `leadscore explain <email>`",
		"ask where the leads are first":              "If the person has not said where their leads are, stop and ask before you copy or run anything",
		"only a lead source the person named":        "Do not go on with a lead source the person did not name.",
		"never invent leads":                         "Never invent, generate or pad leads.",
		"sample leads only for an asked-for demo":    "Use `examples/leads.csv` only when the person asks for a demo, and tell them its leads are made up.",
		"ask for the Apollo mailbox":                 "ask the person for the sending mailbox (its email address or id)",
		"the mailbox can be a teammate's":            "It can be a teammate's mailbox, whoever holds the team's Apollo seat",
		"never pick the mailbox":                     "Never pick one yourself.",
	}
	for duty, phrase := range inSetup {
		if !strings.Contains(flatSetup, flatten(phrase)) {
			t.Errorf("docs/setup.md lacks %s: %q", duty, phrase)
		}
	}
	// Path 1 no longer offers the sample leads as a stand-in for the real ones.
	if strings.Contains(flatSetup, "to try it with made-up leads") {
		t.Error("docs/setup.md must not offer examples/leads.csv in place of the person's leads")
	}
	// Each path asks for the real leads at its copy step, before anything runs.
	for _, path := range []string{"## Path 1: CSV only", "## Path 2: Google Cloud", "## Path 3: Docker (a laptop or a server)"} {
		inOrder(t, path, section(t, setup, path), []string{"where their leads are", "examples/", "leadscore run --dry-run"})
		if !strings.Contains(flatten(section(t, setup, path)), "#before-you-start") {
			t.Errorf("%s must point at Before you start for the lead source", path)
		}
	}
	if !strings.Contains(readFile(t, "setup/apollo/README.md"), "Keep the receiver secret private") {
		t.Error("setup/apollo/README.md must warn to keep the receiver secret private")
	}

	// The step tags stay in docs/setup.md and SKILL.md. Every doc names the
	// published image only, and none puts the receiver secret in a shell profile.
	for _, bad := range []string{"**[agent]**", "**[person]**"} {
		if strings.Contains(readme, bad) {
			t.Errorf("README must not contain %q", bad)
		}
	}
	for _, name := range []string{"README.md", "docs/setup.md", "docs/reference.md", "SKILL.md", "compose.yaml"} {
		doc := readFile(t, name)
		for _, bad := range []string{"private repository", "shell profile (plain binary)", "~/.zshrc", "/dev/urandom"} {
			if strings.Contains(doc, bad) {
				t.Errorf("%s must not contain %q", name, bad)
			}
		}

	}

	// The Google Cloud steps in the runbook's order.
	gcp := section(t, setup, "## Path 2: Google Cloud")
	inOrder(t, "Path 2", gcp, []string{"setup/gcp.sh accounts", "application-default revoke", "setup/gcp.sh bucket",
		"leadscore setup sheet", "never by link", "setup/gcp.sh secrets", "leadscore config push",
		"setup/gcp.sh deploy <image>", "setup/apollo/", "setup/gcp.sh schedule", "leadscore doctor",
		"leadscore run --dry-run", "pushes_enabled: true", "/healthz"})
	if !strings.Contains(gcp, "setup/gcp.sh redeploy --finish-rotation") || strings.Contains(gcp, "## Path 3") {
		t.Error("Path 2 must run from its heading to Path 3, rotation commands included")
	}
	// The Docker steps in the runbook's order.
	docker := section(t, setup, "## Path 3: Docker (a laptop or a server)")
	inOrder(t, "Path 3", docker, []string{"compose.yaml", ".env", "rules check", "Caddy", "docker compose up -d",
		"setup hubspot", "leadscore doctor", "ranked --csv", "run --dry-run", "pushes_enabled: true", "/healthz"})
}

// A missing local build must fail instead of pulling an unrelated registry image.
func TestComposeUsesLocallyBuiltImage(t *testing.T) {
	var compose struct {
		Services map[string]struct {
			Image      string `yaml:"image"`
			PullPolicy string `yaml:"pull_policy"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, "compose.yaml")), &compose); err != nil {
		t.Fatal(err)
	}
	app, ok := compose.Services["leadscore"]
	if !ok || app.Image != "${LEADSCORE_IMAGE:-leadscore:local}" || app.PullPolicy != "never" {
		t.Fatalf("leadscore compose service must use a local-only image, got %+v", app)
	}
}

// Every example leadscore.yml loads; the vendor ones carry the enrich and
// sinks blocks, and the CSV-only pair has no vendor at all and no cold lane.
func TestExampleFiles(t *testing.T) {
	load := func(name string) *config.Config {
		t.Helper()
		data := []byte(readFile(t, filepath.Join("examples", name)))
		c, err := config.Parse(data, "/config", func(string) string { return "" })
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return c
	}
	for _, name := range []string{"leadscore.laptop.yml", "leadscore.server.yml", "leadscore.gcp.yml"} {
		c := load(name)
		if c.Enrich == nil || c.Enrich.Type != "apollo" {
			t.Errorf("%s has no enrich block", name)
		}
		if _, ok := c.Sinks["apollo"]; !ok {
			t.Errorf("%s has no sinks.apollo block", name)
		}
		if !strings.Contains(readFile(t, filepath.Join("examples", name)), "# hubspot: {") {
			t.Errorf("%s does not show the sinks.hubspot block", name)
		}
		if c.PushesEnabled {
			t.Errorf("%s turns pushes on", name)
		}
	}
	csvOnly := load("leadscore.csv-only.yml")
	if csvOnly.Enrich != nil || len(csvOnly.Sinks) != 0 || csvOnly.Store.Type != "sqlite" || csvOnly.PushesEnabled {
		t.Errorf("the CSV-only example uses a vendor or pushes: %+v", csvOnly)
	}
	r, err := rules.Compile([]byte(readFile(t, "examples/rubric.csv-only.yml")))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lanes()) == 0 {
		t.Error("the CSV-only rubric has no lanes")
	}
	for _, l := range r.Lanes() {
		if l.Kind != "export" {
			t.Errorf("the CSV-only rubric's lane %s is %s, not export", l.ID, l.Kind)
		}
	}
}

// The Sheets-on-Docker section of docs/setup.md lists its own actions: the
// Google Cloud runbook's step numbers are not its own, and the Google Cloud
// script must not be run there.
func TestReadmeSheetsOnDockerListsItsSteps(t *testing.T) {
	setup := readFile(t, "docs/setup.md")
	sec := flatten(section(t, setup, "### A Google Sheet on Docker"))
	for _, want := range []string{"enable the Google Sheets and Google Drive APIs", "Create one service account and a JSON key",
		"store.credentials: sa-key.json", "leadscore setup sheet --view", "leadscore setup sheet` (creates the spreadsheet",
		"Create a Cloud Storage bucket for the run lease", "Do not run `setup/gcp.sh`",
		"sudo chgrp 10001 sa-key.json && chmod 640 sa-key.json", "share it only with named people, never by link"} {
		if !strings.Contains(sec, want) {
			t.Errorf("the Sheets-on-Docker section lacks %q", want)
		}
	}
	for _, bad := range []string{"Google Cloud steps 3, 4 and 6", "steps 3 and 4"} {
		if strings.Contains(sec, bad) {
			t.Errorf("the Sheets-on-Docker section points at another path's step numbers: %q", bad)
		}
	}
	flat := flatten(setup)
	for _, want := range []string{"chmod 600 .env", "Never commit `.env`", "Do not edit the protected tabs.",
		"**Rotating a secret. [person]**"} {
		if !strings.Contains(flat, want) {
			t.Errorf("docs/setup.md lacks %q", want)
		}
	}
}
