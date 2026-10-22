// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/merge"
	"github.com/tetriz-ai/leadscore/internal/model"
	"github.com/tetriz-ai/leadscore/internal/store/codec"
	"github.com/tetriz-ai/leadscore/internal/store/sqlite"
)

// sqliteInstall writes a leadscore.yml on a SQLite store holding two leads,
// and returns the config path and the store.
func sqliteInstall(t *testing.T) (string, *sqlite.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "leadscore.yml")
	if err := os.WriteFile(cfg, []byte("version: 1\nstore:\n  type: sqlite\n  path: store.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	m := model.New()
	var rows []merge.Normalized
	for _, r := range []api.InputRow{
		{SourceID: "leads", Headers: []string{"Email", "LinkedIn"},
			Columns: map[string]string{"Email": "ada@acme.example", "LinkedIn": "linkedin.com/in/ada"}},
		{SourceID: "leads", Headers: []string{"LinkedIn"}, Columns: map[string]string{"LinkedIn": "linkedin.com/in/bo"}},
	} {
		n := merge.Normalize(r, nil)
		rows = append(rows, n)
	}
	merge.Apply(m, rows, merge.ApplyCtx{Now: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), RunID: "r1"})
	if err := s.Commit(context.Background(), codec.Encode(m)); err != nil {
		t.Fatal(err)
	}
	return cfg, s
}

func overrideRows(t *testing.T, s *sqlite.Store) []api.Row {
	t.Helper()
	rows, err := s.ReadTable(context.Background(), model.TableOverrides)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestOverridesWriters(t *testing.T) {
	cfg, s := sqliteInstall(t)
	prev, prevUser := now, osUserName
	now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	osUserName = func() (string, error) { return "", errors.New("no name") }
	t.Setenv("LEADSCORE_ACTOR", "")
	t.Cleanup(func() { now, osUserName = prev, prevUser })

	steps := []struct {
		args []string
		want string // stdout contains
	}{
		{[]string{"set-status", "https://www.LinkedIn.com/in/ada/", "unsubscribed"}, "status unsubscribed for ada@acme.example"},
		{[]string{"set-status", "ADA@acme.example", "unsubscribed"}, "nothing to change"},
		{[]string{"set-status", "ada@acme.example", "resubscribe"}, "status resubscribe for ada@acme.example"},
		{[]string{"merge", "linkedin.com/in/bo", "ADA@acme.example"}, "linkedin.com/in/bo same_as ada@acme.example; the next run merges them for good"},
		{[]string{"mark-distinct", "ada@acme.example", "dee@acme.example"}, "ada@acme.example distinct from dee@acme.example (no lead matches yet"},
		{[]string{"retry", "--lane", "warm"}, "retry warm for every lead"},
		{[]string{"retry", "linkedin.com/in/bo"}, "retry every lane for linkedin.com/in/bo"},
	}
	for _, st := range steps {
		code, out, errOut := run(append(st.args, "--config", cfg)...)
		if code != exitOK || !strings.Contains(out, st.want) {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q; want %q", st.args, code, out, errOut, st.want)
		}
	}

	got := overrideRows(t, s)
	want := []model.Override{
		{Person: "ada@acme.example", Action: "status", Value: "resubscribe", Note: "2026-10-07T12:00:00.000Z"},
		{Person: "linkedin.com/in/bo", Action: "same_as", Value: "ada@acme.example"},
		{Person: "ada@acme.example", Action: "distinct", Value: "dee@acme.example"},
		{Person: "*", Action: "retry", Value: "warm", Note: "2026-10-07T12:00:00.000Z"},
		{Person: "linkedin.com/in/bo", Action: "retry", Value: "", Note: "2026-10-07T12:00:00.000Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("Overrides = %v, want %d rows", got, len(want))
	}
	for i, w := range want {
		r := got[i]
		if r["person"] != w.Person || r["action"] != w.Action || r["value"] != w.Value || r["note"] != w.Note {
			t.Errorf("row %d = %v, want %+v", i+1, r, w)
		}
	}
}

// set-status names who asked: --by, else LEADSCORE_ACTOR, else the OS user;
// none of them may be an email.
func TestSetStatusWho(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	prevNow, prevUser := now, osUserName
	now = func() time.Time { return at }
	t.Cleanup(func() { now, osUserName = prevNow, prevUser })
	stamp := model.FormatTime(at)
	for _, c := range []struct {
		name, env, user string
		userErr         bool
		args            []string
		want            string // the row's note; empty means refused
	}{
		{name: "flag", env: "ci-bot", user: "harshit", args: []string{"--by", "ana"}, want: stamp + " by ana"},
		{name: "env", env: "ci-bot", user: "harshit", want: stamp + " by ci-bot"},
		{name: "os user", user: "harshit", want: stamp + " by harshit"},
		{name: "no name", userErr: true, want: stamp},
		{name: "os user with @ is dropped", user: "ana@corp.example", want: stamp},
		{name: "flag email", args: []string{"--by", "ana@acme.example"}},
		{name: "env email", env: "ana@acme.example"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg, s := sqliteInstall(t)
			t.Setenv("LEADSCORE_ACTOR", c.env)
			osUserName = func() (string, error) {
				if c.userErr {
					return "", errors.New("unknown userid 10001")
				}
				return c.user, nil
			}
			code, out, errOut := run(append([]string{"set-status", "ada@acme.example", "blocked", "--config", cfg}, c.args...)...)
			rows := overrideRows(t, s)
			if c.want == "" {
				if code != exitFail || !strings.Contains(errOut, "@") || len(rows) != 0 {
					t.Errorf("exit %d, stderr %q, rows %v; want a refusal naming the @", code, errOut, rows)
				}
				return
			}
			if code != exitOK || len(rows) != 1 || rows[0]["note"] != c.want {
				t.Errorf("exit %d, stdout %q, stderr %q, rows %v; want note %q", code, out, errOut, rows, c.want)
			}
		})
	}
}

func TestOverridesWritersRefuse(t *testing.T) {
	cfg, s := sqliteInstall(t)
	for _, args := range [][]string{
		{"set-status", "ada@acme.example", "deal"},
		{"set-status", "not-a-lead", "unsubscribed"},
		{"merge", "ada@acme.example", "linkedin.com/in/ada"},
		{"merge", "ada@acme.example", "dee@acme.example"}, // a merge names two known leads
		{"merge", "ada@acme.example", "N/A"},
		{"retry", "{{contact.email}}"},
	} {
		code, _, errOut := run(append(args, "--config", cfg)...)
		if code != exitFail || errOut == "" {
			t.Errorf("%v: exit %d, stderr %q; want a refusal", args, code, errOut)
		}
	}
	if rows := overrideRows(t, s); len(rows) != 0 {
		t.Errorf("a refused command wrote %v", rows)
	}
}
