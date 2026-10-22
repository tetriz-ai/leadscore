// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/tetriz-ai/leadscore/adapters/csv"
	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/engine"
	"github.com/tetriz-ai/leadscore/internal/model"
	"github.com/tetriz-ai/leadscore/internal/store/codec"
	"github.com/tetriz-ai/leadscore/internal/store/sqlite"
)

// runInstall is a folder with leadscore.yml on SQLite, a CSV source and a
// small rubric with an export lane.
func runInstall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"leadscore.yml": "version: 1\nstore: { type: sqlite, path: store.db }\nexport: { dir: out }\nsources:\n  - { id: leads, type: csv, path: leads.csv }\n",
		"rubric.yml": "version: 1\nderive:\n  tier:\n    - { when: { field: title, contains: head }, then: 1 }\n    - else: 2\n" +
			"score:\n  contact:\n    - { when: { field: title, contains: head }, points: 5 }\n" +
			"lanes:\n  - { id: list, kind: export, when: { field: tier, lte: 2 }, push: \"export:list\" }\n",
		"leads.csv": "Email,Name,Title\nana@acme.example,Ana A,Head of Ops\nbo@acme.example,Bo B,Clerk\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "leadscore.yml")
}

func cli(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Main(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunStatusRankedExplain(t *testing.T) {
	cfg := runInstall(t)

	code, out, errOut := cli("status", "--config", cfg)
	if code != 0 || !strings.Contains(out, "no run has finished yet") {
		t.Fatalf("status before any run: %d %q %q", code, out, errOut)
	}
	code, out, errOut = cli("run", "--dry-run", "--config", cfg)
	if code != 0 || !strings.Contains(out, "dry run:") || !strings.Contains(out, "totals: 2 lead(s) scored: 2 new") ||
		!strings.Contains(out, "dry run (healthy): 2 lead(s) would be scored") || strings.Contains(out, "@") {
		t.Fatalf("dry run: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := cli("ranked", "--config", cfg); code != 1 || !strings.Contains(errOut, "no run has finished yet") {
		t.Errorf("a dry run writes nothing: %d %q", code, errOut)
	}

	code, out, errOut = cli("run", "--config", cfg)
	if code != 0 || !strings.Contains(out, ": healthy; 2 lead(s) scored") {
		t.Fatalf("run: %d %q %q", code, out, errOut)
	}

	code, out, _ = cli("status", "--config", cfg)
	// Both leads are at acme.example, so the one open problem is the
	// sample-leads warning; the run stays healthy and status names no email.
	if code != 0 || !strings.Contains(out, "last_result      healthy") || !strings.Contains(out, "1 open problem(s):\n  store:sample_leads (since ") ||
		!strings.Contains(out, "    warning: every lead's email is at a .example domain") || strings.Contains(out, "@") {
		t.Errorf("status: %q", out)
	}

	code, out, _ = cli("ranked", "--config", cfg)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 4 || !strings.HasPrefix(lines[0], "lead_id") || !strings.Contains(lines[1], "ana@acme.example") {
		t.Errorf("ranked (highest score first): %q", out)
	}
	if !strings.Contains(lines[0], "company_domain  tier  account_score") {
		t.Errorf("derived columns go after company_domain: %q", lines[0])
	}

	code, out, _ = cli("ranked", "--csv", "--config", cfg)
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if code != 0 || err != nil || len(recs) != 3 || strings.Join(recs[0], ",") !=
		"lead_id,email,linkedin_url,full_name,company_domain,tier,account_score,contact_score,score,status,lane,reasons,rubric_version" {
		t.Errorf("ranked --csv: %v %q", err, out)
	}

	code, out, _ = cli("explain", "ana@acme.example", "--config", cfg)
	for _, want := range []string{"email: ana@acme.example", "lane: list", "tier: 1", "score: 5 (account 0, contact 5)", "+5 contact: title contains head"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("explain lacks %q: %q", want, out)
		}
	}
	if code, _, errOut := cli("explain", "nobody@acme.example", "--config", cfg); code != 1 || !strings.Contains(errOut, "no lead matches") {
		t.Errorf("explain an unknown person: %d %q", code, errOut)
	}
	if strings.Contains(out, "scored with rubric") {
		t.Errorf("no note while the rubric is unchanged: %q", out)
	}
	// The local rubric changes: explain shows the stored verdict with a note.
	rubric := filepath.Join(filepath.Dir(cfg), "rubric.yml")
	text, _ := os.ReadFile(rubric)
	if err := os.WriteFile(rubric, []byte(strings.Replace(string(text), "points: 5", "points: 6", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ = cli("explain", "ana@acme.example", "--config", cfg)
	if !strings.Contains(out, "(scored with rubric r-") || !strings.Contains(out, "so this changes at the next run)") ||
		!strings.Contains(out, "score: 5 (account 0, contact 5)") {
		t.Errorf("explain after a rubric edit: %q", out)
	}
}

// detectorRubric is runInstall's rubric with detectors: a lead one, a company
// one and a first_seen one.
const detectorRubric = "version: 1\ndetectors:\n" +
	"  visits: { kind: count_in_window, event: \"visit_*\", window: 7d, min: 2 }\n" +
	"  busy: { kind: count_in_window, subject: company, event: \"visit_*\", window: 7d, min: 2 }\n" +
	"  new_demo: { kind: first_seen, event: visit_demo, within: 7d }\n" +
	"derive:\n  tier:\n    - { when: { field: title, contains: head }, then: 1 }\n    - else: 2\n" +
	"  hot:\n    - { when: { detector: visits }, then: hot }\n    - else: cold\n" +
	"score:\n  contact:\n    - { when: { field: title, contains: head }, points: 5 }\n" +
	"lanes:\n  - { id: list, kind: export, when: { field: tier, lte: 2 }, push: \"export:list\" }\n"

var detectAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// detectorInstall runs runInstall with detectorRubric, then stores Window
// events and a detect_at the way a run would have. It returns the config
// path and the lead ids by email.
func detectorInstall(t *testing.T, events func(ids map[string]api.LeadID) []model.WindowEvent) (string, map[string]api.LeadID) {
	t.Helper()
	cfg := runInstall(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfg), "rubric.yml"), []byte(detectorRubric), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := cli("run", "--config", cfg); code != 0 {
		t.Fatalf("run: %q %q", out, errOut)
	}
	s, err := sqlite.Open(filepath.Join(filepath.Dir(cfg), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	m, err := codec.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]api.LeadID{}
	for _, row := range m.Ranked {
		ids[row.Email] = row.LeadID
	}
	for _, w := range events(ids) {
		m.Put(model.TableWindowEvents, w)
	}
	m.SetState(engine.DetectAtKey, model.FormatTime(detectAt))
	if err := s.Commit(ctx, codec.Encode(m, model.TableWindowEvents, model.TableState)); err != nil {
		t.Fatal(err)
	}
	return cfg, ids
}

// section returns explain's detectors section.
func section(out string) string {
	_, after, _ := strings.Cut(out, "detectors")
	return "detectors" + after
}

// explain prints each detector, fired or not, with its evidence: events by
// kind, time, lead id and page. A company detector shows a colleague by lead
// id, never by email; an email in a page is masked; event keys never print.
func TestExplainPrintsDetectorEvidence(t *testing.T) {
	cfg, ids := detectorInstall(t, func(ids map[string]api.LeadID) []model.WindowEvent {
		ana, bo := ids["ana@acme.example"], ids["bo@acme.example"]
		return []model.WindowEvent{
			{EventKey: "visit:visit_pricing:day:ana@acme.example:1", Subject: "lead", LeadID: ana, Domain: "acme.example",
				Kind: "visit_pricing", At: detectAt.Add(-2 * time.Hour),
				Attrs: map[string]string{"page": "https://ourproduct.example/pricing?ref=ana@acme.example"}},
			{EventKey: "visit:visit_site:day:ana@acme.example:2", Subject: "lead", LeadID: ana, Domain: "acme.example",
				Kind: "visit_site", At: detectAt.Add(-time.Hour)},
			{EventKey: "visit:visit_site:day:bo@acme.example:3", Subject: "lead", LeadID: bo, Domain: "acme.example",
				Kind: "visit_site", At: detectAt.Add(-30 * time.Minute)},
			{EventKey: "visit:visit_site:day:ana@acme.example:old", Subject: "lead", LeadID: ana, Domain: "acme.example",
				Kind: "visit_site", At: detectAt.Add(-8 * 24 * time.Hour)}, // outside every window
		}
	})
	code, out, errOut := cli("explain", "ana@acme.example", "--config", cfg)
	if code != 0 {
		t.Fatalf("explain: %q %q", out, errOut)
	}
	sec := section(out)
	for _, want := range []string{
		"detectors (worked out again at the last detection time, 2026-09-01T12:00:00.000Z):",
		"  visits (count_in_window): fired\n    2 matching event(s) in the window, 2 needed\n",
		"    - visit_pricing at 2026-09-01T10:00:00.000Z, lead " + string(ids["ana@acme.example"]) + ", page https://ourproduct.example/pricing?ref=a",
		"  busy (count_in_window, company acme.example): fired\n    3 matching event(s) in the window, 2 needed\n",
		"    - visit_site at 2026-09-01T11:30:00.000Z, lead " + string(ids["bo@acme.example"]) + "\n",
		"  new_demo (first_seen): not fired\n    never seen\n",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("explain lacks %q:\n%s", want, sec)
		}
	}
	if strings.Contains(sec, "@acme.example") || strings.Contains(out, "bo@") || strings.Contains(out, "visit:") {
		t.Errorf("explain printed an email or an event key:\n%s", out)
	}
	if strings.Contains(sec, "2026-08-24") {
		t.Errorf("an event outside the window was listed:\n%s", sec)
	}
	if !strings.Contains(out, "reasons:") || strings.Index(out, "reasons:") > strings.Index(out, "detectors") {
		t.Errorf("detectors go after the reasons:\n%s", out)
	}
	if strings.Contains(errOut, "visit") {
		t.Errorf("evidence reached stderr: %q", errOut)
	}
}

// Without detect_at (a run from before it was saved), explain uses Health
// last_run_at and says so. When the stored verdict used another rubric, it
// shows no evidence and says why.
func TestExplainDetectorFallbacks(t *testing.T) {
	cfg, _ := detectorInstall(t, func(map[string]api.LeadID) []model.WindowEvent { return nil })
	s, err := sqlite.Open(filepath.Join(filepath.Dir(cfg), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m, err := codec.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	m.Delete(model.TableState, []string{engine.DetectAtKey})
	if err := s.Commit(ctx, codec.Encode(m, model.TableState)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	_, out, _ := cli("explain", "ana@acme.example", "--config", cfg)
	if !strings.Contains(out, "detectors (the last run saved no detection time; worked out again at its start, last_run_at ") ||
		!strings.Contains(out, "  visits (count_in_window): not fired\n") {
		t.Errorf("fallback to last_run_at:\n%s", out)
	}

	rubric := filepath.Join(filepath.Dir(cfg), "rubric.yml")
	if err := os.WriteFile(rubric, []byte(strings.Replace(detectorRubric, "points: 5", "points: 6", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ = cli("explain", "ana@acme.example", "--config", cfg)
	if !strings.Contains(out, "detectors: not shown: this verdict used rubric r-") || strings.Contains(out, "visits (") {
		t.Errorf("rubric mismatch:\n%s", out)
	}
}

// An unhealthy run exits 1, so a hosted job execution shows the failure.
func TestRunExitsOneWhenUnhealthy(t *testing.T) {
	cfg := runInstall(t)
	rubric := filepath.Join(filepath.Dir(cfg), "rubric.yml")
	if err := os.WriteFile(rubric, []byte("version: 1\nlanes:\n  - { id: seq, kind: cold, when: { field: receiver_only, eq: false }, push: \"nosuchsink:x\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := cli("run", "--config", cfg); code != 1 || !strings.Contains(out, "lane_sink_unregistered:seq") {
		t.Errorf("got %d %q", code, out)
	}
}

// Stored text that a spreadsheet would read as a formula is quoted in
// `ranked --csv`; numbers are left alone. Control characters never reach the
// terminal from status, ranked or explain.
func TestPrintedStoredTextIsSafe(t *testing.T) {
	cfg := runInstall(t)
	leads := filepath.Join(filepath.Dir(cfg), "leads.csv")
	text := "Email,Name,Title\nana@acme.example,\"=HYPERLINK(\"\"http://x\"\",\"\"y\"\")\",Head of Ops\nbo@acme.example,Bo \x1b[31mB,Clerk\n"
	if err := os.WriteFile(leads, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := cli("run", "--config", cfg); code != 0 {
		t.Fatalf("run: %q %q", out, errOut)
	}
	_, out, _ := cli("ranked", "--csv", "--config", cfg)
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(recs) != 3 {
		t.Fatalf("%v %q", err, out)
	}
	if name := recs[1][3]; name != `'=HYPERLINK("http://x","y")` {
		t.Errorf("full_name cell %q must be quoted", name)
	}
	if score := recs[1][8]; score != "5" {
		t.Errorf("score cell %q must stay a number", score)
	}
	for _, args := range [][]string{{"ranked"}, {"explain", "bo@acme.example"}, {"status"}} {
		_, out, _ := cli(append(args, "--config", cfg)...)
		if strings.ContainsRune(out, '\x1b') {
			t.Errorf("%v printed a control character: %q", args, out)
		}
	}
	// Nor from detector evidence: a stored event's kind and page.
	cfg, _ = detectorInstall(t, func(ids map[string]api.LeadID) []model.WindowEvent {
		return []model.WindowEvent{{EventKey: "k", Subject: "lead", LeadID: ids["ana@acme.example"], Domain: "acme.example",
			Kind: "visit_\x1b[2Jx", At: detectAt.Add(-time.Hour), Attrs: map[string]string{"page": "https://x.example/\x1b[31mred"}}}
	})
	_, out, _ = cli("explain", "ana@acme.example", "--config", cfg)
	if !strings.Contains(out, "visit_�[2Jx at") || strings.ContainsRune(out, '\x1b') {
		t.Errorf("explain's evidence printed a control character: %q", out)
	}
}

// The read-only commands never create the SQLite file: on a fresh folder each
// says no run has finished yet, and the folder still has no store. status
// exits 0 (that is the state it reports); the others have nothing to show
// and exit 1. doctor still warns store:not_created.
func TestReadOnlyCommandsCreateNoStore(t *testing.T) {
	cfg := runInstall(t)
	db := filepath.Join(filepath.Dir(cfg), "store.db")
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"status"}, exitOK},
		{[]string{"ranked"}, exitFail},
		{[]string{"ranked", "--csv"}, exitFail},
		{[]string{"explain", "ana@acme.example"}, exitFail},
		{[]string{"facts"}, exitFail},
		{[]string{"facts", "--csv"}, exitFail},
	} {
		code, out, errOut := cli(append(tc.args, "--config", cfg)...)
		if code != tc.code || !strings.Contains(out+errOut, "no run has finished yet; run `leadscore run` first") {
			t.Errorf("%v: %d %q %q", tc.args, code, out, errOut)
		}
		if _, err := os.Stat(db); !os.IsNotExist(err) {
			t.Fatalf("%v created the SQLite file: %v", tc.args, err)
		}
	}
	if _, out, _ := cli("doctor", "--config", cfg); !strings.Contains(out, "warn  store: store:not_created:") {
		t.Errorf("doctor: %q", out)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("doctor created the SQLite file: %v", err)
	}
}
