// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	"strings"

	"github.com/tetriz-ai/leadscore/internal/api"
)

// An undeclared squashed name may be a column only the data shows (a CSV
// header, a Sheet or Companies column, an enrichment fact), so it is accepted
// with a warning. One that is a near miss of a name the rubric already knows
// is far more likely a typo, and a typo never matches anything, so it fails
// at load. Declaring the name under fields keeps it.

// undeclared checks an undeclared squashed name: an error for a near miss of
// a known name, otherwise a warning (once per name). display is the name as
// written (company.<x> for a company fact). It reports whether the name may
// be read.
func (c *compiler) undeclared(name, display string, company bool, a at) bool {
	known := c.leadNames()
	if company {
		known = c.companyNames()
	}
	if near, ok := nearMiss(name, known); ok {
		if company {
			c.errf(a, "%s is not a known company field; did you mean %s? If %s is a real company column in your data, declare it under fields (%s: { level: company })", display, near, name, name)
		} else {
			c.errf(a, "%s is not a known field; did you mean %s? If %s is a real column in your data, declare it under fields (%s: { type: text })", display, near, name, name)
		}
		return false
	}
	where := "a column"
	if company {
		where = "a company column"
	}
	w := LoadError{Line: a.line(), Field: a.path, Msg: display + " is not declared; it must come from " + where + " in your data"}
	// One warning per name, at its first line in the file: blocks compile in
	// another order than they are written.
	if i, ok := c.warned[display]; !ok {
		c.warned[display] = len(c.warnings)
		c.warnings = append(c.warnings, w)
	} else if prev := c.warnings[i].Line; prev == 0 || (w.Line > 0 && w.Line < prev) {
		c.warnings[i] = w
	}
	return true
}

// leadNames maps each spelling a lead-level rule may mean to the name to
// write: built-in and declared lead fields, status, derived names, and every
// header spelling (an alias names its field).
func (c *compiler) leadNames() map[string]string {
	m := map[string]string{"status": "status"}
	for n := range c.leadFields {
		m[n] = n
	}
	for n := range c.deriveByName {
		m[n] = n
	}
	for sq, target := range c.allAliases() {
		m[sq] = target
	}
	return m
}

// companyNames is leadNames for company.<x>: company fields, rollups,
// company-level derived names and the header spellings of company fields, each
// keyed without the prefix.
func (c *compiler) companyNames() map[string]string {
	m := map[string]string{}
	for n := range c.companyFields {
		m[n] = "company." + n
	}
	for n := range c.rollupNames {
		m[n] = "company." + n
	}
	for n, d := range c.deriveByName {
		if d.company {
			m[n] = "company." + n
		}
	}
	for sq, target := range c.allAliases() {
		if strings.HasPrefix(target, "company.") {
			m[sq] = target
		}
	}
	return m
}

// allAliases is the built-in alias table plus the rubric's own, which wins.
// Event-row spellings (`at`) are left out: no rule reads them.
func (c *compiler) allAliases() map[string]string {
	m := map[string]string{}
	for sq, target := range api.BuiltinAliases() {
		if target != "at" {
			m[sq] = target
		}
	}
	for sq, target := range c.aliases {
		m[sq] = target
	}
	return m
}

// nearMiss returns the name to suggest when name is a near miss of a known
// spelling: an edit distance of 1 when either is 5 characters or shorter,
// otherwise up to 2, and not equal. A spelling is compared as written and
// squashed (sources_seen and sourcesseen). The closest wins; ties go to the
// alphabetically first suggestion, so the message is stable.
func nearMiss(name string, known map[string]string) (string, bool) {
	best, bestD := "", 0
	for spelling, target := range known {
		for _, s := range []string{spelling, api.SquashHeader(spelling)} {
			limit := 2
			if min(len(name), len(s)) <= 5 {
				limit = 1
			}
			d := editDistance(name, s)
			if d == 0 || d > limit {
				continue
			}
			if best == "" || d < bestD || (d == bestD && target < best) {
				best, bestD = target, d
			}
		}
	}
	return best, best != ""
}

// editDistance is the Damerau-Levenshtein distance between two ASCII names:
// the fewest insertions, deletions, substitutions and swaps of two adjacent
// characters that turn a into b.
func editDistance(a, b string) int {
	// The unrestricted algorithm (Lowrance-Wagner), so a swap may be edited
	// again: "ca" to "abc" is 2.
	inf := len(a) + len(b)
	w := len(b) + 2
	d := make([]int, (len(a)+2)*w)
	at := func(i, j int) *int { return &d[i*w+j] }
	*at(0, 0) = inf
	for i := 0; i <= len(a); i++ {
		*at(i+1, 0), *at(i+1, 1) = inf, i
	}
	for j := 0; j <= len(b); j++ {
		*at(0, j+1), *at(1, j+1) = inf, j
	}
	last := map[byte]int{} // the last row each character of a was seen in
	for i := 1; i <= len(a); i++ {
		db := 0
		for j := 1; j <= len(b); j++ {
			i1, j1 := last[b[j-1]], db
			cost := 1
			if a[i-1] == b[j-1] {
				cost, db = 0, j
			}
			*at(i+1, j+1) = min(
				*at(i, j)+cost, // substitute
				*at(i+1, j)+1,  // insert
				*at(i, j+1)+1,  // delete
				*at(i1, j1)+(i-i1-1)+1+(j-j1-1), // swap
			)
		}
		last[a[i-1]] = i
	}
	return *at(len(a)+1, len(b)+1)
}
