// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/model"
)

// Pushes go in batches of 25 leads, each preceded by the ReRead hook; the
// leads a re-read changes are folded again before their batch.
func TestReReadBeforeEachBatch(t *testing.T) {
	w := newWorld(t, clerks(30)...)
	n := 0
	var late api.LeadID
	w.reread = func(r *Run) ([]api.LeadID, error) {
		n++
		if n == 2 { // an opt-out arrived during batch 1, for a lead of batch 2
			late = w.id("p29@p29.example")
			o := r.Model.Outcomes[model.Key(late)]
			o.LeadID, o.UnsubscribedAt, o.UnsubscribedOrigin = late, r.Now(), "event"
			r.Model.Put(model.TableOutcomes, o)
			return []api.LeadID{late}, nil
		}
		return nil, nil
	}
	res, out := w.mustRun()
	if n != 2 {
		t.Fatalf("ReRead ran %d times, want once per batch (2)\n%s", n, out)
	}
	if res.Pushed != 29 {
		t.Errorf("pushed %d, want 29", res.Pushed)
	}
	for _, c := range w.fake.Calls() {
		if c.Key.LeadID == late {
			t.Fatal("a lead who opted out mid-run was pushed in the next batch")
		}
	}
	if r := w.push("p29@p29.example", "seq-b", "push"); r["state"] != stateCancelled || r["called_at"] != "" {
		t.Errorf("its row %v", r)
	}
}

// A failed re-read stops pushing for the rest of the run.
func TestFailedReReadStopsPushing(t *testing.T) {
	w := newWorld(t, clerks(30)...)
	n := 0
	w.reread = func(*Run) ([]api.LeadID, error) {
		n++
		if n == 2 {
			return nil, errors.New("event log unreachable")
		}
		return nil, nil
	}
	res, _ := w.mustRun()
	if got := len(w.fake.Calls()); got != 25 {
		t.Errorf("calls %d, want only the first batch (25)", got)
	}
	if !hasKey(res.Problems, "step_failed:reread") || res.Healthy {
		t.Errorf("problems %v", res.Problems)
	}
	for _, r := range w.rows(model.TablePushes) {
		if r["intent_run"] != "" {
			t.Errorf("row left with intent_run: %v", r)
		}
	}
}

// The Overrides tab is read again before each batch: a row a person adds
// during the run blocks a lead in a later batch.
func TestOverridesReReadMidRun(t *testing.T) {
	w := newWorld(t, clerks(30)...)
	w.pushesOff()
	added := false
	w.fake.Before(func(context.Context, api.StepRequest) {
		if !added {
			added = true
			w.override("P28@P28.example", "status", "unsubscribed", "")
		}
	})
	w.mustRun()
	for _, c := range w.fake.Calls() {
		if c.Lead.Emails[0] == "p28@p28.example" {
			t.Fatal("a lead opted out in Overrides mid-run was pushed")
		}
	}
	if n := len(w.fake.Calls()); n != 29 {
		t.Errorf("calls %d", n)
	}
}

// No pushing while the import has a backlog (the run's pushing rules).
func TestNoPushWithBacklog(t *testing.T) {
	w := newWorld(t, clerks(3)...)
	w.config("pushes_enabled: true", "pushes_enabled: true\ningest_chunk_rows: 1")
	res, _ := w.mustRun()
	if len(w.fake.Calls()) != 0 || len(w.rows(model.TablePushes)) != 0 {
		t.Fatalf("pushed with a backlog: %v", calls(w.fake))
	}
	if !hasKey(res.Problems, "ingest_backlog") {
		t.Errorf("problems %v", res.Problems)
	}
	w.mustRun()
	w.mustRun() // the backlog is gone after the third chunk
	if n := len(w.fake.Calls()); n != 3 {
		t.Errorf("after the backlog cleared: %d calls", n)
	}
}

// SIGTERM mid-batch: the call in flight finishes and is recorded, no new
// call starts, and the ledger is consistent: no intent_run left, called_at
// on exactly the steps called.
func TestSIGTERMMidBatchLeavesLedgerConsistent(t *testing.T) {
	w := newWorld(t, clerks(10)...)
	stop := make(chan struct{})
	n := 0
	w.fake.Before(func(context.Context, api.StepRequest) {
		n++
		if n == 3 {
			closeStop(stop)
		}
	})
	res, _ := w.mustRun(stopAfter(stop))
	if got := len(w.fake.Calls()); got != 3 {
		t.Fatalf("%d calls, want 3 (the third was in flight)", got)
	}
	if res.Pushed != 3 || !hasKey(res.Problems, "run_stopped") {
		t.Errorf("pushed %d, problems %v", res.Pushed, res.Problems)
	}
	called := map[string]bool{}
	for _, c := range w.fake.Calls() {
		called[string(c.Key.LeadID)] = true
	}
	rows := w.rows(model.TablePushes)
	if len(rows) != 10 {
		t.Fatalf("%d rows, want one per lead of the batch", len(rows))
	}
	for _, r := range rows {
		switch {
		case r["intent_run"] != "":
			t.Errorf("intent_run left: %v", r)
		case called[r["lead_id"]] && (r["state"] != stateDone || r["called_at"] == ""):
			t.Errorf("a called step not recorded: %v", r)
		case !called[r["lead_id"]] && (r["state"] != statePending || r["called_at"] != ""):
			t.Errorf("a step never called has %v", r)
		}
	}
	// The next run finishes the rest, each lead once.
	w.fake.Before(nil)
	w.mustRun()
	if got := len(w.fake.Calls()); got != 10 {
		t.Errorf("%d calls in all, want 10", got)
	}
}

// A stalled run that lost its lease mid-batch stops at the next lease check
// and writes nothing more: its post-batch write and phase 2 are refused. The
// pre-batch write's intent_run is what the next run turns into called_at.
func TestStalledRunWritesNothingAfterLosingTheLease(t *testing.T) {
	w := newWorld(t, clerks(30)...)
	w.pushesOff()
	taken := false
	w.fake.Before(func(context.Context, api.StepRequest) {
		if !taken {
			taken = true
			w.setLease("successor", time.Now().Add(time.Hour))
		}
	})
	_, out, err := w.run()
	if err == nil || !errors.Is(err, api.ErrLeaseLost) && !strings.Contains(err.Error(), "lease") {
		t.Fatalf("the stalled run: %v\n%s", err, out)
	}
	if got := len(w.fake.Calls()); got != 25 {
		t.Errorf("%d calls: the stalled run must not start a second batch", got)
	}
	rows := w.rows(model.TablePushes)
	if len(rows) != 25 {
		t.Fatalf("%d rows, want the 25 of the pre-batch write", len(rows))
	}
	for _, r := range rows {
		if r["intent_run"] == "" || r["called_at"] != "" || r["state"] != statePending {
			t.Errorf("the stalled run wrote after losing the lease: %v", r)
		}
	}
	if w.state("lease_owner") != "successor" {
		t.Error("the stalled run released its successor's lease")
	}
	// The successor (once the lease is free) sees the intents as called.
	w.setLease("", time.Time{})
	w.fake.Before(nil)
	w.mustRun()
	for _, r := range w.rows(model.TablePushes) {
		if r["intent_run"] != "" || r["state"] != stateDone {
			t.Errorf("after the successor: %v", r)
		}
	}
	if n := w.fake.Count("push"); n != 30 {
		t.Errorf("%d vendor objects, want one per lead", n)
	}
}

// A dry run plans lanes (lookups assumed to pass) and prints them, and
// writes, calls and looks up nothing.
func TestDryRunPlannedLanes(t *testing.T) {
	w := newWorld(t,
		"ana@acme.example,Ana A,Head of Ops,acme.example",
		"bo@beta.example,Bo B,Clerk,beta.example",
		"cy@cyan.example,Cy C,Head of Sales,cyan.example")
	w.pushesOff()
	w.reply("cy@cyan.example", "replied_positive")
	lk := w.lookup("hubspot")
	before := len(w.rows(model.TableLog))
	_, out := w.mustRun(dry)
	if !strings.Contains(out, "planned lanes: seq-a 1, seq-b 1, warm 1") {
		t.Errorf("dry-run output:\n%s", out)
	}
	if !strings.Contains(out, "status new -> replied_positive; lane seq-a -> warm") {
		t.Errorf("Cy's new planned lane is not reported:\n%s", out)
	}
	if len(w.apollo.Calls())+len(w.fake.Calls())+len(w.hubspot.Calls()) != 0 || len(lk.Looked()) != 0 {
		t.Error("a dry run called a vendor")
	}
	if len(w.rows(model.TablePushes)) != 0 || len(w.rows(model.TableLog)) != before {
		t.Error("a dry run wrote")
	}
}

// Configuration alone cannot establish that Apollo unsubscribe events arrive.
// Only the Apollo sink waits; other sinks keep working, and a persisted
// unsubscribe receipt enables subsequent Apollo pushes.
func TestApolloPushWaitsForUnsubscribeEvidence(t *testing.T) {
	for _, received := range []bool{false, true} {
		t.Run(fmt.Sprintf("received=%v", received), func(t *testing.T) {
			w := newWorld(t, "ana@acme.example,Ana,head,acme.example", "bo@beta.example,Bo,clerk,beta.example")
			w.config("  hubspot: {}\n", "")
			if received {
				err := w.store().Commit(t.Context(), []api.TableWrite{{Table: model.TableState, Op: api.OpUpsert, Key: []string{"key"}, Rows: []api.Row{{"key": "last_received:unsubscribed", "value": "2026-10-01T09:00:00.000Z"}}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			res, _ := w.mustRun()
			want := 0
			if received {
				want = 2
			}
			if got := len(w.apollo.Calls()); got != want {
				t.Errorf("Apollo calls %d, want %d", got, want)
			}
			if len(w.fake.Calls()) != 1 {
				t.Error("unrelated sink did not push")
			}
			rows := listed(w, "list")
			if len(rows) != 2 || rows["ana@acme.example"] == nil || rows["bo@beta.example"] == nil {
				t.Errorf("export list lost leads while Apollo waited: %v", rows)
			}
			if got := hasKey(res.Problems, "apollo-key:no_optout_flag"); got == received {
				t.Errorf("problem present=%v, received=%v", got, received)
			}
		})
	}
}

func TestApolloConfiguredHubSpotRequiresUsableLookup(t *testing.T) {
	for _, mode := range []string{"available", "unregistered", "failed", "omitted sinks"} {
		t.Run(mode, func(t *testing.T) {
			w := newWorld(t, "ana@acme.example,Ana,head,acme.example")
			if mode == "omitted sinks" {
				w.config("  hubspot: {}\n", "")
				w.config("  apollo: {}\n", "")
			}
			if mode == "unregistered" {
				delete(w.lookups, "hubspot")
			}
			if mode == "failed" {
				w.lookup("hubspot").FailAllLookups(errors.New("unavailable"))
			}
			w.mustRun()
			want := 0
			if mode == "available" {
				want = 2
			}
			if got := len(w.apollo.Calls()); got != want {
				t.Errorf("Apollo calls %d, want %d", got, want)
			}
		})
	}
}

func TestApolloBlockedPushRetriesAfterRealUnsubscribeIntake(t *testing.T) {
	w := newWorld(t, "ana@acme.example,Ana,head,acme.example", "bo@beta.example,Bo,head,beta.example")
	w.config("  hubspot: {}\n", "")
	w.config("pushes_enabled: true", "pushes_enabled: true\nreplies: receiver\nreceiver:\n  public_url: https://receiver.example")
	w.mustRun()
	if len(w.apollo.Calls()) != 0 || len(w.rows(model.TablePushes)) != 0 {
		t.Fatal("blocked push called a sink or consumed ledger state")
	}
	w.appendEvents(replyRaw("email_unsubscribed", "bo@beta.example", "", time.Now()))
	res, _ := w.mustRun()
	if hasKey(res.Problems, "apollo-key:no_optout_flag") {
		t.Error("resolved problem persisted")
	}
	if w.state("last_received:unsubscribed") == "" {
		t.Fatal("unsubscribe receipt not saved")
	}
	if got := len(w.apollo.Calls()); got != 2 {
		t.Fatalf("calls %d, want Ana's two steps", got)
	}
	for _, call := range w.apollo.Calls() {
		if call.Lead.Emails[0] != "ana@acme.example" {
			t.Fatal("unsubscribed person was pushed")
		}
	}
	w.mustRun()
	if got := len(w.apollo.Calls()); got != 2 {
		t.Errorf("cold push retried after success: %d calls", got)
	}
}
