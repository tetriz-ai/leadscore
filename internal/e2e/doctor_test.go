// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sheetsapi "google.golang.org/api/sheets/v4"

	"github.com/tetriz-ai/leadscore/adapters/apollo"
	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/cli"
	fakehub "github.com/tetriz-ai/leadscore/internal/fakes/hubspot"
	"github.com/tetriz-ai/leadscore/internal/merge"
	"github.com/tetriz-ai/leadscore/internal/model"
	"github.com/tetriz-ai/leadscore/internal/receiver"
	"github.com/tetriz-ai/leadscore/internal/store/codec"
	"github.com/tetriz-ai/leadscore/internal/store/sheets"
)

// otherAccount is a service account the spreadsheet is not shared with.
const otherAccount = "leadscore-other@e2e.iam.gserviceaccount.com"

// doctor runs `leadscore doctor` on the install, with the vendor blocks
// pointed at the fakes through the suite's loopback client. Doctor reads the
// wall clock, not the suite's, so callers assert on failures and their keys,
// never on how many warnings there are.
func (in *install) doctor() (int, string) {
	in.t.Helper()
	restore := cli.SetTestHTTPClient(in.client)
	defer restore()
	var out, errb bytes.Buffer
	code := cli.Main([]string{"doctor", "--config", filepath.Join(in.dir, "leadscore.yml")}, &out, &errb)
	in.t.Logf("doctor (exit %d):\n%s%s", code, out.String(), errb.String())
	return code, out.String()
}

// editConfig replaces from with to in leadscore.yml; from must be there.
func (in *install) editConfig(from, to string) {
	in.t.Helper()
	path := filepath.Join(in.dir, "leadscore.yml")
	text, err := os.ReadFile(path)
	if err != nil {
		in.t.Fatal(err)
	}
	if !bytes.Contains(text, []byte(from)) {
		in.t.Fatalf("leadscore.yml has no %q:\n%s", from, text)
	}
	in.write("leadscore.yml", strings.Replace(string(text), from, to, 1))
}

// commitModel loads the store, changes the model with f, and commits only
// the given tables, as a person editing those tabs would.
func (in *install) commitModel(f func(m *model.Model), tables ...string) {
	in.t.Helper()
	ctx := context.Background()
	m, err := codec.Load(ctx, in.store)
	if err != nil {
		in.t.Fatal(err)
	}
	f(m)
	if err := in.store.Commit(ctx, codec.Encode(m, tables...)); err != nil {
		in.t.Fatal(err)
	}
}

// snapshot reads every fixed table of the store.
func (in *install) snapshot() map[string][]api.Row {
	in.t.Helper()
	out := map[string][]api.Row{}
	for _, d := range model.Tables {
		if !d.Pattern {
			out[d.Name] = in.rows(d.Name)
		}
	}
	out["nurture"] = in.rows(model.ExportTable("nurture"))
	return out
}

// Doctor catches a broken install. A vendor install on each store is green
// after its first run: doctor exits 0 with no failure. Then each case breaks
// one thing and doctor must exit 1, naming the problem under the check that
// owns it, and leave every table as it was.
func TestDoctorCatchesABrokenInstall(t *testing.T) {
	// Doctor reads the variables a run's adapters and receiver read.
	for _, v := range []string{receiver.PreviousSecretVar, "K_SERVICE", "CLOUD_RUN_JOB"} {
		t.Setenv(v, "")
	}
	t.Setenv(receiver.SecretVar, receiverSecret)

	cases := []struct {
		name       string
		sheetsOnly bool
		check, key string // doctor must print FAIL  <check>: <key>
		breakIt    func(t *testing.T, in *install)
	}{
		{name: "an Overrides status with a typo", check: "overrides", key: "status_conflict:",
			breakIt: func(t *testing.T, in *install) {
				in.commitModel(func(m *model.Model) {
					if err := m.Put(model.TableOverrides, model.Override{Person: "anna.weber@kranlogistik.example",
						Action: merge.ActionStatus, Value: "unsubscribd"}); err != nil {
						t.Fatal(err)
					}
				}, model.TableOverrides)
			}},
		{name: "Apollo refuses the key", check: "apollo-key", key: "apollo-key:auth",
			breakIt: func(t *testing.T, _ *install) { t.Setenv(apollo.KeyVariable, "a-key-apollo-revoked") }},
		{name: "a sequence renamed in Apollo", check: "apollo-sequences", key: "apollo-sequences:hot-visitors",
			breakIt: func(_ *testing.T, in *install) { in.apollo.RenameSequence("seq-demo", "Demo visitors (old)") }},
		{name: "the HubSpot pipeline is gone", check: "hubspot", key: "hubspot:pipeline",
			breakIt: func(_ *testing.T, in *install) {
				in.editConfig("pipeline: "+`"`+fakehub.PipelineLabel+`"`, `pipeline: "Retired pipeline"`)
			}},
		{name: "ledger rows deleted", check: "store", key: "ledger_shrank",
			breakIt: func(t *testing.T, in *install) {
				// As a person deleting the tab's first row would.
				ledger := in.rows(model.TablePushes)
				if len(ledger) == 0 {
					t.Fatal("run 1 left no ledger rows to delete")
				}
				row := api.Row{"lead_id": ledger[0]["lead_id"], "lane_id": ledger[0]["lane_id"], "step": ledger[0]["step"]}
				if err := in.store.Commit(t.Context(), []api.TableWrite{{Table: model.TablePushes, Op: api.OpDelete,
					Key: []string{"lead_id", "lane_id", "step"}, Rows: []api.Row{row}}}); err != nil {
					t.Fatal(err)
				}
				if got := len(in.rows(model.TablePushes)); got != len(ledger)-1 {
					t.Fatalf("the ledger has %d rows after deleting one of %d", got, len(ledger))
				}
			}},
		{name: "a rubric that does not compile", check: "rubric", key: "rubric_invalid:compile",
			breakIt: func(_ *testing.T, in *install) { in.write("rubric.yml", "version: 1\nlanes: nope\n") }},
		{name: "no receiver secret", check: "receiver-secret", key: "secret_missing:" + receiver.SecretVar,
			breakIt: func(t *testing.T, _ *install) { t.Setenv(receiver.SecretVar, "") }},
		{name: "recalculation not hourly", sheetsOnly: true, check: "sheets", key: "sheets:recalc",
			breakIt: func(t *testing.T, in *install) {
				// As a person changing File > Settings > Calculation would.
				req := &sheetsapi.BatchUpdateSpreadsheetRequest{Requests: []*sheetsapi.Request{{
					UpdateSpreadsheetProperties: &sheetsapi.UpdateSpreadsheetPropertiesRequest{
						Properties: &sheetsapi.SpreadsheetProperties{AutoRecalc: "ON_CHANGE"}, Fields: "autoRecalc"}}}}
				svc := in.store.(*sheets.Store).Services().Sheets
				if _, err := svc.Spreadsheets.BatchUpdate(in.sheetID, req).Context(t.Context()).Do(); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "the lease bucket is missing", sheetsOnly: true, check: "lease", key: "lease:bucket",
			breakIt: func(_ *testing.T, in *install) { in.editConfig("lease_bucket: lease,", "lease_bucket: lease-deleted,") }},
		{name: "the run account is not an editor", sheetsOnly: true, check: "sheet-access", key: "sheet-access:" + otherAccount,
			breakIt: func(_ *testing.T, in *install) {
				// A hosted install whose run account the spreadsheet is not
				// shared with. No hosting.region: the hosting check stops at
				// its config failure and never calls Google.
				in.editConfig("version: 1\n", "version: 1\nhosting: { project: e2e, run_account: "+otherAccount+" }\n")
			}},
	}

	for _, kind := range []string{"sqlite", "sheets"} {
		t.Run(kind, func(t *testing.T) {
			t.Run("green after run 1", func(t *testing.T) {
				in := newInstall(t, kind, vendorV)
				in.run(t1)
				before := in.snapshot()
				if code, out := in.doctor(); code != 0 || strings.Contains(out, "FAIL") {
					t.Errorf("doctor on a working install: exit %d, want 0 with no FAIL", code)
				}
				if !reflect.DeepEqual(before, in.snapshot()) {
					t.Error("doctor changed the store")
				}
			})
			for _, c := range cases {
				if c.sheetsOnly && kind != "sheets" {
					continue
				}
				t.Run(c.name, func(t *testing.T) {
					in := newInstall(t, kind, vendorV)
					in.run(t1)
					c.breakIt(t, in)
					before := in.snapshot()
					code, out := in.doctor()
					if want := "FAIL  " + c.check + ": " + c.key; code != 1 || !strings.Contains(out, want) {
						t.Errorf("doctor: exit %d, want 1 with a line starting %q", code, want)
					}
					if !reflect.DeepEqual(before, in.snapshot()) {
						t.Error("doctor changed the store")
					}
				})
			}
		})
	}
}
