// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package detect evaluates the rubric's detectors: the built-in kinds
// count_in_window, first_seen and change, and kinds a build registers, over
// each lead's and each company's Window events. Windows are (now - window, now]
// in UTC. Event kinds, and the kind names themselves, are compared lowercased.
//
// Evaluate is what a run calls; it keeps only whether each detector fired.
// Explain works the same detectors out again for one lead and keeps the
// evidence, for `leadscore explain`. Evidence holds stored event rows, which
// can carry personal data: it is for printing through logredact, never for
// Log, Health or stderr.
package detect

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/merge"
	"github.com/tetriz-ai/leadscore/internal/model"
	"github.com/tetriz-ai/leadscore/internal/rules"
)

// Built-in kinds.
const (
	KindCount     = "count_in_window"
	KindFirstSeen = "first_seen"
	KindChange    = "change"
)

// MaxEvidence is the most events one detector's Evidence lists; More counts
// the rest.
const MaxEvidence = 20

// Evidence is one detector's result for one subject, and what it rests on.
type Evidence struct {
	Name    string
	Kind    string
	Subject string // lead or company
	Domain  string // the company, for a company detector
	Fired   bool
	// Events, oldest first: for count_in_window the matching events in the
	// window (Count is how many); for a registered kind the events it named,
	// keeping only ids among the events it was given. At most MaxEvidence;
	// More is how many more there were.
	Events []model.WindowEvent
	Count  int
	More   int
	// FirstSeen, for first_seen: the earliest matching kind and when it was
	// first seen, or nil if never.
	FirstSeen *Seen
	// Change, for change: the fact's last change, or nil if it has none.
	Change *Change
	// Problem says why the detector could not be worked out: a registered
	// kind missing from the build, or a company detector for a lead with no
	// company domain.
	Problem string
}

// Seen is a first-seen kind and time.
type Seen struct {
	Kind string
	At   time.Time
}

// Change is a company fact's change: the value it replaced, the value now,
// and when that was set.
type Change struct {
	Field, From, To string
	At              time.Time
}

// Evaluate works out which detectors fired for each lead (lead subject) and
// each company (company subject) and returns them as the evaluator's
// DetectorResults: only fired detectors are listed. leads are the live leads,
// with their company domains. A lead's events are the Window events of its
// whole merge family; a company's are every Window event at its domain, lead
// events included.
//
// A registered kind that is missing from the build, or whose factory fails,
// is skipped (it does not fire) and named in errs.
func Evaluate(specs []rules.DetectorSpec, m *model.Model, leads []api.LeadRef, now time.Time) (rules.DetectorResults, []error) {
	res := rules.DetectorResults{Leads: map[api.LeadID]map[string]bool{}, Companies: map[string]map[string]bool{}}
	if len(specs) == 0 {
		return res, nil
	}
	now = now.UTC()
	byLead, byDomain := index(m)
	domains := map[string]bool{}
	for d := range byDomain {
		domains[d] = true
	}
	for _, l := range leads {
		if l.Domain != "" {
			domains[l.Domain] = true
		}
	}
	for k := range m.CompanyFacts {
		domains[string(k)] = true
	}
	custom, problems := build(specs)
	var errs []error
	for _, s := range specs {
		if p, ok := problems[s.Name]; ok {
			errs = append(errs, fmt.Errorf("detector %s %s; it does not fire", s.Name, p))
		}
	}

	fire := func(into map[string]map[string]bool, at, name string) {
		if into[at] == nil {
			into[at] = map[string]bool{}
		}
		into[at][name] = true
	}
	for _, s := range specs {
		if !builtIn(s.Kind) && custom[s.Name] == nil {
			continue
		}
		if s.Subject == "company" {
			for d := range domains {
				cf := m.CompanyFacts[model.Key(d)]
				subj := api.Subject{Domain: d}
				if eval(s, custom[s.Name], subj, byDomain[d], cf.FirstSeen, cf, now).Fired {
					fire(res.Companies, d, s.Name)
				}
			}
			continue
		}
		for i := range leads {
			l := &leads[i]
			p := m.People[model.Key(l.ID)]
			cf := m.CompanyFacts[model.Key(l.Domain)]
			subj := api.Subject{Lead: l, Domain: l.Domain}
			if eval(s, custom[s.Name], subj, byLead[l.ID], p.FirstSeen, cf, now).Fired {
				if res.Leads[l.ID] == nil {
					res.Leads[l.ID] = map[string]bool{}
				}
				res.Leads[l.ID][s.Name] = true
			}
		}
	}
	return res, errs
}

// Explain works out every detector for one live lead, as Evaluate does with
// the same now: lead-subject detectors for the lead, company-subject ones
// for its company. It returns one Evidence per detector, in spec order.
func Explain(specs []rules.DetectorSpec, m *model.Model, lead api.LeadRef, now time.Time) []Evidence {
	if len(specs) == 0 {
		return nil
	}
	now = now.UTC()
	byLead, byDomain := index(m)
	custom, problems := build(specs)
	out := make([]Evidence, 0, len(specs))
	for _, s := range specs {
		var ev Evidence
		switch {
		case problems[s.Name] != "":
			ev.Problem = problems[s.Name]
		case s.Subject == "company" && lead.Domain == "":
			ev.Problem = "has no company to look at: the lead has no company domain"
		case s.Subject == "company":
			cf := m.CompanyFacts[model.Key(lead.Domain)]
			ev = eval(s, custom[s.Name], api.Subject{Domain: lead.Domain}, byDomain[lead.Domain], cf.FirstSeen, cf, now)
		default:
			p := m.People[model.Key(lead.ID)]
			cf := m.CompanyFacts[model.Key(lead.Domain)]
			l := lead
			ev = eval(s, custom[s.Name], api.Subject{Lead: &l, Domain: lead.Domain}, byLead[lead.ID], p.FirstSeen, cf, now)
		}
		ev.Name, ev.Kind, ev.Subject = s.Name, s.Kind, s.Subject
		if s.Subject == "company" {
			ev.Domain = lead.Domain
		}
		out = append(out, ev)
	}
	return out
}

// index groups the Window events, in key order, by live lead and by domain.
func index(m *model.Model) (byLead map[api.LeadID][]model.WindowEvent, byDomain map[string][]model.WindowEvent) {
	byLead = map[api.LeadID][]model.WindowEvent{}
	byDomain = map[string][]model.WindowEvent{}
	keys := make([]string, 0, len(m.WindowEvents))
	for k := range m.WindowEvents {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		w := m.WindowEvents[model.Key(k)]
		if w.LeadID != "" {
			live := merge.Live(m, w.LeadID)
			byLead[live] = append(byLead[live], w)
		}
		if w.Domain != "" {
			byDomain[w.Domain] = append(byDomain[w.Domain], w)
		}
	}
	return byLead, byDomain
}

// build makes each registered-kind detector, by detector name. problems
// names, by detector name, a kind missing from the build or a factory that
// failed.
func build(specs []rules.DetectorSpec) (custom map[string]api.Detector, problems map[string]string) {
	custom, problems = map[string]api.Detector{}, map[string]string{}
	for _, s := range specs {
		if builtIn(s.Kind) {
			continue
		}
		f, ok := api.DetectorFactory(s.Kind)
		if !ok {
			problems[s.Name] = fmt.Sprintf("has kind %q, which this build does not register", s.Kind)
			continue
		}
		d, err := f(s.Params)
		if err != nil {
			problems[s.Name] = fmt.Sprintf("could not be built: kind %q: %v", s.Kind, err)
			continue
		}
		custom[s.Name] = d
	}
	return custom, problems
}

func builtIn(kind string) bool {
	switch kind {
	case KindCount, KindFirstSeen, KindChange:
		return true
	}
	return false
}

// eval runs one detector for one subject: its window events, its first-seen
// times by kind, and its company's facts. It returns whether it fired, with
// the evidence.
func eval(s rules.DetectorSpec, custom api.Detector, subj api.Subject, ws []model.WindowEvent,
	firstSeen map[string]time.Time, cf model.CompanyFact, now time.Time) Evidence {
	var ev Evidence
	switch s.Kind {
	case KindCount:
		in := inWindowOf(ws, s.Event, s.Window, now)
		ev.Count, ev.Fired = len(in), len(in) >= s.Min
		ev.setEvents(in)
		return ev
	case KindFirstSeen:
		kind, first := firstSeenOf(firstSeen, s.Event)
		if !first.IsZero() {
			ev.FirstSeen = &Seen{Kind: kind, At: first}
		}
		ev.Fired = !first.IsZero() && inWindow(first, s.Within, now)
		return ev
	case KindChange:
		ev.Fired = changed(cf, s.Field, s.Within, s.From, s.To, now)
		if cur, ok := cf.Facts[s.Field]; ok {
			if prev, had := cf.Previous[s.Field]; had {
				ev.Change = &Change{Field: s.Field, From: prev.Value, To: cur.Value, At: cur.At}
			}
		}
		return ev
	}
	evs := make([]api.Event, 0, len(ws))
	byID := make(map[api.EventID]model.WindowEvent, len(ws))
	for _, w := range ws {
		id := api.EventID(w.EventKey)
		evs = append(evs, api.Event{ID: id, Kind: w.Kind, Domain: w.Domain, At: w.At, Attrs: w.Attrs})
		byID[id] = w
	}
	fired, ids := custom.Evaluate(subj, evs, now)
	ev.Fired = fired
	// Keep only ids among the events passed in, once each: a detector's
	// output is not trusted to name stored events.
	var named []model.WindowEvent
	for _, id := range ids {
		if w, ok := byID[id]; ok {
			named = append(named, w)
			delete(byID, id)
		}
	}
	ev.setEvents(named)
	return ev
}

// setEvents sorts ws oldest first (then by key) and keeps MaxEvidence.
func (ev *Evidence) setEvents(ws []model.WindowEvent) {
	sort.SliceStable(ws, func(i, j int) bool {
		if !ws[i].At.Equal(ws[j].At) {
			return ws[i].At.Before(ws[j].At)
		}
		return ws[i].EventKey < ws[j].EventKey
	})
	if len(ws) > MaxEvidence {
		ev.More = len(ws) - MaxEvidence
		ws = ws[:MaxEvidence]
	}
	ev.Events = ws
}

// matches reports whether an event kind matches a detector's `event`: equal,
// or, for a pattern ending in `*`, starting with the part before it. Both are
// compared lowercased.
func matches(pattern, kind string) bool {
	p, k := strings.ToLower(pattern), strings.ToLower(kind)
	if pre, ok := strings.CutSuffix(p, "*"); ok {
		return strings.HasPrefix(k, pre)
	}
	return p == k
}

// inWindow reports at in (now - window, now].
func inWindow(at time.Time, window time.Duration, now time.Time) bool {
	return at.After(now.Add(-window)) && !at.After(now)
}

// inWindowOf returns the events of a matching kind in (now - window, now].
func inWindowOf(ws []model.WindowEvent, event string, window time.Duration, now time.Time) []model.WindowEvent {
	var out []model.WindowEvent
	for _, w := range ws {
		if matches(event, w.Kind) && inWindow(w.At, window, now) {
			out = append(out, w)
		}
	}
	return out
}

// firstSeenOf is the earliest first-seen time over the kinds matching event,
// with its kind, or the zero time. A tie goes to the kind first in order.
func firstSeenOf(firstSeen map[string]time.Time, event string) (string, time.Time) {
	var kind string
	var first time.Time
	for k, t := range firstSeen {
		if !matches(event, k) || t.IsZero() {
			continue
		}
		if first.IsZero() || t.Before(first) || (t.Equal(first) && k < kind) {
			kind, first = k, t
		}
	}
	return kind, first
}

// changed reports whether a stored company fact changed within `within`: the
// fact holds a value set in the window (facts.<f>.at) that replaced a
// different one (previous.<f>), matching from and to when given. A fact set
// for the first time is not a change.
func changed(cf model.CompanyFact, field string, within time.Duration, from, to *string, now time.Time) bool {
	cur, ok := cf.Facts[field]
	prev, had := cf.Previous[field]
	if !ok || !had || cur.Value == "" || !inWindow(cur.At, within, now) || sameText(cur.Value, prev.Value) {
		return false
	}
	if from != nil && !sameText(*from, prev.Value) {
		return false
	}
	if to != nil && !sameText(*to, cur.Value) {
		return false
	}
	return true
}

// sameText compares text as the rubric does: trimmed,
// Unicode NFC, ignoring case.
func sameText(a, b string) bool {
	return rules.NormText(a) == rules.NormText(b)
}
