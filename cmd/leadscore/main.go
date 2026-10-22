// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command leadscore is the release binary: the built-in adapters plus the CLI.
// A custom build copies this file and adds an import for its own adapter package.
package main

import (
	"github.com/tetriz-ai/leadscore"

	// Built-in adapters in public packages (adapters/...) register here with
	// blank imports. The built-in stores are internal, so
	// they register through the root package instead.
	_ "github.com/tetriz-ai/leadscore/adapters/csv"
	_ "github.com/tetriz-ai/leadscore/adapters/hubspot"
	_ "github.com/tetriz-ai/leadscore/adapters/sheetsource"
)

func main() { leadscore.Main() }
