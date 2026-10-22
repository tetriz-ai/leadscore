// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesCheck(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yml")
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(good, []byte("version: 1\nlanes: []\nderive:\n  segment:\n    - else: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("version: 1\nlanes:\n  - { id: a, kind: warm, push: export:x, when: { field: status, eq: new } }\nlimits: { timezone: Nowhere }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run("rules", "check", good)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, good+": ok (version r-") ||
		!strings.Contains(stdout, ":4: warning: derive.segment: shadows the input field segment from here on") {
		t.Errorf("good: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	code, stdout, stderr = run("rules", "check", bad)
	want := "leadscore rules check: 2 problems\n" +
		bad + ":3: lanes[0].kind: kind is cold, non-cold or export\n" +
		bad + `:4: limits.timezone: unknown timezone "Nowhere"; use an IANA name such as UTC or Asia/Kolkata` + "\n"
	if code != exitFail || stdout != "" || stderr != want {
		t.Errorf("bad: exit %d, stdout %q, stderr %q\nwant %q", code, stdout, stderr, want)
	}

	if code, _, stderr := run("rules", "check", filepath.Join(dir, "none.yml")); code != exitFail || !strings.Contains(stderr, "none.yml") {
		t.Errorf("missing file: exit %d, stderr %q", code, stderr)
	}
}

// A field typo fails with the line and a suggestion; an undeclared column warns
// with its line and still passes.
func TestRulesCheckFieldNames(t *testing.T) {
	dir := t.TempDir()
	typo := filepath.Join(dir, "typo.yml")
	column := filepath.Join(dir, "column.yml")
	body := "version: 1\nlanes: []\nscore:\n  contact:\n    - { when: { field: %s, present: true }, points: 1 }\n"
	if err := os.WriteFile(typo, []byte(strings.Replace(body, "%s", "titel", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(column, []byte(strings.Replace(body, "%s", "maintool", 1)), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run("rules", "check", typo)
	want := "leadscore rules check: 1 problem\n" +
		typo + ":5: score.contact[0].when.field: titel is not a known field; did you mean title? " +
		"If titel is a real column in your data, declare it under fields (titel: { type: text })\n"
	if code != exitFail || stdout != "" || stderr != want {
		t.Errorf("typo: exit %d, stdout %q, stderr %q\nwant %q", code, stdout, stderr, want)
	}

	code, stdout, stderr = run("rules", "check", column)
	wantOut := column + ":5: warning: score.contact[0].when.field: maintool is not declared; it must come from a column in your data\n"
	if code != exitOK || stderr != "" || !strings.HasPrefix(stdout, wantOut) || !strings.Contains(stdout, column+": ok (version r-") {
		t.Errorf("column: exit %d, stdout %q, stderr %q\nwant prefix %q", code, stdout, stderr, wantOut)
	}
}

// The shipped example rubric compiles.
func TestRulesCheckExample(t *testing.T) {
	if code, stdout, stderr := run("rules", "check", "../../examples/rubric.yml"); code != exitOK {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
