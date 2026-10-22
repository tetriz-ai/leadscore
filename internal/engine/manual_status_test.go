// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/events"
	"github.com/tetriz-ai/leadscore/internal/merge"
	"github.com/tetriz-ai/leadscore/internal/model"
)

// manualLogs returns the manual_status Log rows, failing on any that names
// an email.
func manualLogs(w *world) []api.Row {
	w.t.Helper()
	var out []api.Row
	for _, r := range w.rows(model.TableLog) {
		if r["kind"] != logManualStatus {
			continue
		}
		if strings.Contains(r["message"], "@") || r["email"] != "" {
			w.t.Errorf("a manual_status line carries an email: %v", r)
		}
		out = append(out, r)
	}
	return out
}

// newManualLog runs once and returns the one manual_status line it wrote,
// failing unless there is exactly one.
func newManualLog(t *testing.T, w *world) api.Row {
	t.Helper()
	before := len(manualLogs(w))
	w.mustRun()
	logs := manualLogs(w)
	if len(logs)-before != 1 {
		t.Fatalf("the run wrote %d manual_status lines, want 1: %v", len(logs)-before, logs[before:])
	}
	return logs[len(logs)-1]
}

func setStatusBy(w *world, person, status, by string, at time.Time) {
	w.t.Helper()
	w.edit(func(m *model.Model) {
		if _, err := merge.SetStatusBy(m, person, status, at, by); err != nil {
			w.t.Fatal(err)
		}
	})
}

// Every manual status change is logged once, by the run: a set, a change, a
// conflict and a removal, with the CLI's request time and who when the row
// has them, and none for a hand edit.
func TestManualStatusChangesAreLogged(t *testing.T) {
	w := newWorld(t,
		"ana@acme.example,Ana A,Head of Ops,acme.example",
		"bo@beta.example,Bo B,Head of Ops,beta.example")
	w.config("pushes_enabled: true", "pushes_enabled: false")
	w.mustRun()
	if logs := manualLogs(w); len(logs) != 0 {
		t.Fatalf("no manual status yet, but logged %v", logs)
	}
	ana := string(w.id("ana@acme.example"))
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	stamp := model.FormatTime(at)

	setStatusBy(w, "ana@acme.example", "replied_negative", "ana-ops", at)
	l := newManualLog(t, w)
	for _, want := range []string{"none -> replied_negative", "Overrides row 1", "requested " + stamp + " by ana-ops"} {
		if !strings.Contains(l["message"], want) {
			t.Errorf("set: message %q lacks %q", l["message"], want)
		}
	}
	if l["lead_id"] != ana || l["level"] != "info" {
		t.Errorf("set: line %v", l)
	}
	if got := w.outcome("ana@acme.example")["manual_status"]; got != "replied_negative" {
		t.Errorf("Outcomes.manual_status = %q", got)
	}

	// A run with nothing new logs nothing.
	before := len(manualLogs(w))
	w.mustRun()
	if n := len(manualLogs(w)); n != before {
		t.Errorf("an unchanged status was logged again (%d lines, want %d)", n, before)
	}

	// A change from the CLI with no known who: the time, no " by ".
	setStatusBy(w, "ana@acme.example", "replied_positive", "", at.Add(time.Hour))
	l = newManualLog(t, w)
	if m := l["message"]; !strings.Contains(m, "replied_negative -> replied_positive") ||
		!strings.Contains(m, "requested "+model.FormatTime(at.Add(time.Hour))) || strings.Contains(m, " by ") {
		t.Errorf("change: message %q", m)
	}

	// A hand-typed second row conflicts: blocked.
	w.override("ana@acme.example", "status", "replied_neutral", "")
	l = newManualLog(t, w)
	if m := l["message"]; !strings.Contains(m, "replied_positive -> blocked") || !strings.Contains(m, "conflicting status rows 1, 2") {
		t.Errorf("conflict: message %q", m)
	}

	// Removing them: none.
	setStatusBy(w, "ana@acme.example", "none", "ana-ops", at.Add(2*time.Hour))
	l = newManualLog(t, w)
	if m := l["message"]; !strings.Contains(m, "blocked -> none") {
		t.Errorf("removal: message %q", m)
	}
	if got := w.outcome("ana@acme.example")["manual_status"]; got != "" {
		t.Errorf("Outcomes.manual_status after none = %q", got)
	}

	// A hand edit on another lead: no request time, no who.
	w.override("bo@beta.example", "status", "replied_neutral", "called on Monday")
	l = newManualLog(t, w)
	if m := l["message"]; !strings.Contains(m, "none -> replied_neutral") || strings.Contains(m, "requested") || strings.Contains(m, " by ") {
		t.Errorf("hand edit: message %q", m)
	}
	if l["lead_id"] != string(w.id("bo@beta.example")) {
		t.Errorf("hand edit: line %v", l)
	}
}

// A manual status on an opted-out lead is logged, saying it has no effect.
func TestManualStatusOnUnsubscribedLeadSaysNoEffect(t *testing.T) {
	w := newWorld(t, "ana@acme.example,Ana A,Head of Ops,acme.example")
	w.config("pushes_enabled: true", "pushes_enabled: false")
	w.mustRun()
	w.event("ana@acme.example", api.Event{Kind: "optout", Email: "ana@acme.example", Origin: events.OriginApolloLookup})
	setStatusBy(w, "ana@acme.example", "replied_positive", "ana-ops", time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))
	l := newManualLog(t, w)
	if m := l["message"]; !strings.Contains(m, "none -> replied_positive") || !strings.Contains(m, "no effect: unsubscribed wins") {
		t.Errorf("message %q", m)
	}
	if s := w.outcome("ana@acme.example")["status"]; s != statusUnsubscribed {
		t.Errorf("status %q, want unsubscribed", s)
	}
	// A manual unsubscribed is not "no effect".
	setStatusBy(w, "ana@acme.example", "unsubscribed", "", time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC))
	if m := newManualLog(t, w)["message"]; strings.Contains(m, "no effect") {
		t.Errorf("message %q", m)
	}
}

// The first run after an upgrade records the manual statuses it finds
// without logging them; a lead it has never folded is still logged, and so
// is every later change.
func TestManualStatusFirstRunAfterUpgrade(t *testing.T) {
	w := newWorld(t,
		"ana@acme.example,Ana A,Head of Ops,acme.example",
		"bo@beta.example,Bo B,Head of Ops,beta.example")
	w.config("pushes_enabled: true", "pushes_enabled: false")
	w.override("ana@acme.example", "status", "replied_negative", "")
	w.override("bo@beta.example", "status", "blocked", "")
	w.mustRun()
	// Make the store look as the previous version left it: no manual_status
	// and no State key.
	w.edit(func(m *model.Model) {
		for _, o := range m.Outcomes {
			o.ManualStatus = ""
			m.Put(model.TableOutcomes, o)
		}
		m.Delete(model.TableState, []string{manualStatusTrackedKey})
	})
	w.write("leads.csv", csvText("Email,Name,Title,Domain",
		"ana@acme.example,Ana A,Head of Ops,acme.example",
		"bo@beta.example,Bo B,Head of Ops,beta.example",
		"cy@gamma.example,Cy C,Head of Ops,gamma.example"))
	w.override("cy@gamma.example", "status", "replied_neutral", "")
	before := len(manualLogs(w))
	w.mustRun()
	logs := manualLogs(w)[before:]
	if len(logs) != 1 || logs[0]["lead_id"] != string(w.id("cy@gamma.example")) {
		t.Errorf("the upgrade run logged %v, want only Cy's new lead", logs)
	}
	if got := w.outcome("ana@acme.example")["manual_status"]; got != "replied_negative" {
		t.Errorf("Ana's manual_status = %q, recorded without a log", got)
	}
	if w.state(manualStatusTrackedKey) == "" {
		t.Error("the upgrade run did not record that it tracks manual statuses")
	}
	setStatusBy(w, "ana@acme.example", "none", "", time.Now())
	if m := newManualLog(t, w)["message"]; !strings.Contains(m, "replied_negative -> none") {
		t.Errorf("after the upgrade: %q", m)
	}
}
