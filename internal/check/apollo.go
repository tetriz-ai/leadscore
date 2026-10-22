// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package check

import (
	"context"
	"os"
	"strings"

	"github.com/tetriz-ai/leadscore/adapters/apollo"
	"github.com/tetriz-ai/leadscore/internal/api"
	"github.com/tetriz-ai/leadscore/internal/config"
	"github.com/tetriz-ai/leadscore/internal/model"
)

func init() { Register(apolloKey{getenv: os.Getenv}) }

// apolloKey is the `apollo-key` check. It signs in
// with Apollo's free auth-health call, never an enrichment call, so running
// it every run spends no credits. A missing key is the secrets check's to
// report; a block the client refuses is apollo-key:config, a key Apollo
// refuses (401, 403, or is_logged_in false) is apollo-key:auth, and a call
// that got no answer is the warning apollo-key:unreachable. It also blocks a
// team that sends only through Apollo, until an unsubscribe webhook has been
// received, when Apollo's contacts have no opt-out flag to look up
// (apollo.ContactOptOutFlag).
type apolloKey struct{ getenv func(string) string }

func (apolloKey) Name() string { return "apollo-key" }
func (apolloKey) InRun() bool  { return true }

func (a apolloKey) Run(ctx context.Context, env Env) []Problem {
	block := apolloBlock(env.Config)
	if block == nil {
		return nil
	}
	var out []Problem
	if key := a.getenv(apollo.KeyVariable); strings.TrimSpace(key) != "" {
		c, err := apollo.NewClientWithKey(block, key)
		if err != nil {
			out = append(out, Problem{Key: "apollo-key:config",
				Message: "the Apollo block in leadscore.yml cannot be used: " + err.Error(),
				Fix:     "fix the enrich or sinks.apollo block"})
		} else {
			err = c.AuthHealth(ctx)
		}
		switch {
		case c == nil:
		case apollo.KeyRefused(err):
			out = append(out, Problem{Key: "apollo-key:auth",
				Message: "Apollo refuses the key: " + err.Error(),
				Fix:     "check the key in " + apollo.KeyVariable + " (Secret Manager or .env)"})
		case err != nil:
			// A rate limit, a 5xx or a network failure says nothing about
			// the key: a warning under its own key.
			out = append(out, Problem{Key: "apollo-key:unreachable", Warning: true,
				Message: "Apollo's auth-health call did not answer, so the key was not checked: " + err.Error(),
				Fix:     "nothing if it clears on the next run; otherwise check Apollo's status and the network"})
		}
	}
	// Doctor assumes the standard HubSpot adapter; the push gate also checks
	// the engine's actual lookup registry before allowing the exemption.
	if _, sends := env.Config.Sinks["apollo"]; sends && ApolloPushNeedsOptOutEvidence(env.Config, env.Model, true) {
		out = append(out, Problem{Key: "apollo-key:no_optout_flag",
			Message: "Apollo's contacts carry no opt-out flag leadscore can look up, and no HubSpot opt-out lookup is available, " +
				"so Apollo pushes wait until an unsubscribe webhook has been received",
			Fix: "configure an Apollo unsubscribe workflow and verify its event reaches the receiver, or configure a HubSpot sink with its opt-out lookup"})
	}
	return out
}

// ApolloPushNeedsOptOutEvidence reports whether Apollo pushes must wait.
// A configured receiver alone proves nothing: a stored unsubscribe receipt is
// required unless a configured HubSpot sink supplies its pre-push lookup.
// Lookup failures still block the affected leads through the normal push gate.
func ApolloPushNeedsOptOutEvidence(c *config.Config, m *model.Model, hubspotLookup bool) bool {
	if apollo.ContactOptOutFlag {
		return false
	}
	if c != nil {
		if _, configured := c.Sinks["hubspot"]; configured && hubspotLookup {
			return false
		}
	}
	return m == nil || m.StateValue("last_received:unsubscribed") == ""
}

// apolloBlock is the leadscore.yml block an Apollo client is built from: the
// enrich block when it uses Apollo, else sinks.apollo; nil when the install
// does not use Apollo.
func apolloBlock(c *config.Config) api.Config {
	if c == nil {
		return nil
	}
	if c.Enrich != nil && c.Enrich.Type == "apollo" {
		return c.Enrich.Block
	}
	if b, ok := c.Sinks["apollo"]; ok {
		if b == nil {
			b = api.Config{}
		}
		return b
	}
	return nil
}
