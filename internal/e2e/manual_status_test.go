// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/tetriz-ai/leadscore/internal/merge"
	"github.com/tetriz-ai/leadscore/internal/model"
	"github.com/tetriz-ai/leadscore/internal/store/codec"
)

// editOverrides changes Overrides as a person or the CLI would: load the
// store, change the model, commit only Overrides.
func (in *install) editOverrides(f func(m *model.Model)) {
	in.t.Helper()
	ctx := context.Background()
	m, err := codec.Load(ctx, in.store)
	if err != nil {
		in.t.Fatal(err)
	}
	f(m)
	if err := in.store.Commit(ctx, codec.Encode(m, model.TableOverrides)); err != nil {
		in.t.Fatal(err)
	}
}

// A manual status change is logged once on both stores: the CLI's row with
// its request time and who, a hand-typed row with neither.
func TestManualStatusLogOnBothStores(t *testing.T) {
	for _, kind := range stores {
		t.Run(kind, func(t *testing.T) {
			in := newInstall(t, kind, csvOnlyC)
			in.editOverrides(func(m *model.Model) {
				if _, err := merge.SetStatusBy(m, lea, "replied_negative", in.clock.Now(), "ana-ops"); err != nil {
					t.Fatal(err)
				}
			})
			in.run(t1)
			in.editOverrides(func(m *model.Model) {
				m.Put(model.TableOverrides, model.Override{Person: anna, Action: "status", Value: "blocked", Note: "typed in the sheet"})
			})
			in.run(t2)

			ids := in.leadIDs()
			got := map[string]string{}
			for _, r := range in.rows(model.TableLog) {
				if r["kind"] != "manual_status" {
					continue
				}
				if strings.Contains(r["message"], "@") || r["email"] != "" {
					t.Errorf("a manual_status line carries an email: %v", r)
				}
				if _, dup := got[r["lead_id"]]; dup {
					t.Errorf("lead %s logged twice", r["lead_id"])
				}
				got[r["lead_id"]] = r["message"]
			}
			if len(got) != 2 {
				t.Fatalf("manual_status lines %v, want one for Lea and one for Anna", got)
			}
			if m := got[ids[lea]]; !strings.Contains(m, "none -> replied_negative") ||
				!strings.Contains(m, "requested "+model.FormatTime(t0)+" by ana-ops") {
				t.Errorf("Lea's line %q", m)
			}
			if m := got[ids[anna]]; !strings.Contains(m, "none -> blocked") || strings.Contains(m, "requested") {
				t.Errorf("Anna's line %q: a hand edit has no request time or who", m)
			}
			if o := in.outcome(lea); o["manual_status"] != "replied_negative" {
				t.Errorf("Lea's Outcomes %v", o)
			}
		})
	}
}
