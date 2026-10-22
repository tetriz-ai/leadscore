// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"slices"
	"strings"
	"testing"

	"github.com/tetriz-ai/leadscore/internal/check"
	"github.com/tetriz-ai/leadscore/internal/model"
)

// A run on the shipped sample leads (every email at a .example domain) stays
// healthy but warns, in Health, that the leads are made up. Once one real
// lead arrives, the next run clears the warning.
func TestSampleLeadsWarnInHealth(t *testing.T) {
	for _, kind := range stores {
		t.Run(kind, func(t *testing.T) {
			in := newInstall(t, kind, csvOnlyC)
			res := in.run(t1)
			if !slices.Contains(res.Problems, check.SampleLeadsKey) {
				t.Fatalf("run on the sample leads raised %v, want %s", res.Problems, check.SampleLeadsKey)
			}
			v, ok := in.healthProblem(check.SampleLeadsKey)
			if !ok || !strings.HasPrefix(v, "warning: ") || !strings.Contains(v, ".example") {
				t.Errorf("Health %s = %q, %v; want a warning naming .example", check.SampleLeadsKey, v, ok)
			}
			if strings.Contains(v, "@") {
				t.Errorf("Health names a person: %q", v)
			}

			in.addLead("ravi.menon@example.com,Ravi Menon,Head of Operations,Menon Freight,example.com,300,India,Series B,,,,,")
			res = in.run(t2)
			if slices.Contains(res.Problems, check.SampleLeadsKey) {
				t.Errorf("a real domain among the leads still raised %s", check.SampleLeadsKey)
			}
			if _, ok := in.healthProblem(check.SampleLeadsKey); ok {
				t.Errorf("Health still holds %s after a real lead arrived", check.SampleLeadsKey)
			}
		})
	}
}

// healthProblem returns the Health problem row's value under key.
func (in *install) healthProblem(key string) (string, bool) {
	in.t.Helper()
	for _, r := range in.rows(model.TableHealth) {
		if r["kind"] == "problem" && r["key"] == key {
			return r["value"], true
		}
	}
	return "", false
}
