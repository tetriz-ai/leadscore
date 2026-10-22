# Contributing

Bug reports, fixes and new adapters are welcome. Open an issue first for anything larger than
a fix, so the change can be agreed before you write it. Report a security problem privately,
as [SECURITY.md](SECURITY.md) says, not as an issue.

## Build and test

You need Go 1.26 or later. GitHub Actions remain disabled. The retained check
configuration covers Go 1.26 and 1.27; run the applicable checks locally.

```sh
go build ./... && go vet ./... && go test -race ./...
```

The tests need no account and no key. Vendors and Google Cloud are faked in `internal/fakes`.
Also build and vet for Windows:

```sh
GOOS=windows go build ./... && GOOS=windows go vet ./...
```

## Live checks

A few tests call real services. Each skips unless its variable is set, and the retained check
configuration sets none of them.

| Variable | What it runs |
|---|---|
| `LEADSCORE_LIVE_APOLLO` | Read-only Apollo calls. Set it to an env file holding `APOLLO_API_KEY` and `APOLLO_MAILBOX_ID`. |
| `LEADSCORE_LIVE_SHEETS` | The Sheets store against real Google Sheets: 20,000 leads and a year of events in a scratch spreadsheet. Set it to `1`, or to a service-account key file. `LEADSCORE_LIVE_SHEETS_OWNER` names the gcloud login that creates the spreadsheet. |
| `LEADSCORE_LIVE_CLOUDRUN` | The whole Google Cloud setup in a throwaway project. It also needs `LEADSCORE_LIVE_PROJECT`, `LEADSCORE_LIVE_IMAGE` and `LEADSCORE_LIVE_DENY_PROJECTS`, the projects it must never touch. The comment on `TestLiveCloudRun` in `cloudrun_live_test.go` lists the steps before it. |

Use a throwaway account or project, never one a real install uses.

## Local checks

| Check | Command |
|---|---|
| Format | `gofmt -l .` prints nothing |
| Lint | `golangci-lint run` with golangci-lint v2.14.0 |
| Shell | `shellcheck setup/gcp.sh scripts/*.sh testdata/gcloud/gcloud testdata/compose/smoke.sh` with shellcheck 0.11.0 |
| Dockerfile | `hadolint --ignore DL3018 --ignore DL3066 Dockerfile` |
| Vulnerabilities | `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` on Go 1.27.x |
| Third-party licenses | `go install github.com/google/go-licenses/v2@v2.0.1`, then `scripts/third-party-licenses.sh --check` |
| Docker | `docker build -t leadscore:ci .`, then `testdata/compose/smoke.sh leadscore:ci`: `docker compose up`, a webhook, and timer runs. Linux only: it needs sudo and port 8080 free |

Install the linter with
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`, on Go 1.26 or
later. Install shellcheck and hadolint with `brew install shellcheck hadolint` on macOS, or your
package manager and hadolint's GitHub releases page on Linux.

Do not silence a finding inline. Fix it, or change `.golangci.yml` with a comment saying why.

After a dependency change, run `scripts/third-party-licenses.sh` to refresh
`third_party/licenses/`, then update the module list in `NOTICE`.

## Layout

| Path | What lives there |
|---|---|
| module root (package `leadscore`) | the public API: plug-in interfaces, types, errors, the adapter registry, `Run`, `Main` |
| `cmd/leadscore/` | the CLI binary |
| `internal/api` | the public types (re-exported by the root), the built-in header aliases |
| `internal/config` | loading `leadscore.yml`, `config get`, `config set-hosting` |
| `internal/check` | the `doctor` check framework and the checks |
| `internal/cli` | the commands, `doctor` among them |
| `internal/model` | the in-memory model of the store's tables |
| `internal/store/codec` | maps the model to table writes, loads a store and checks its schema version |
| `internal/store/sqlite` | the built-in SQLite store (WAL, lease row, event log) |
| `internal/store/sheets` | the built-in Google Sheets store (one batchUpdate per commit, monthly `Events` tabs, Cloud Storage lease file) and `setup sheet` |
| `internal/fakes/sheets`, `internal/fakes/gcs` | in-memory fakes of Google Sheets, Drive and Cloud Storage for tests |
| `internal/rules` | the rubric compiler and evaluator: YAML rules compiled to CEL |
| `internal/merge` | turns input rows into one lead per person: header aliases, identities, `same_as` merges, the Overrides tab |
| `internal/receiver` | `leadscore serve`: the Apollo webhook receiver, its write queue, `/healthz` and the Docker run timer |
| `internal/receiver/auth` | the constant-time secret check |
| `internal/engine` | the run: lease, sources and chunked merge, scoring, the two saves, `Ranked`, `Health`, lanes and the ledger, export lists and their CSVs, the Sheet view |
| `internal/logredact` | log redaction: logs carry ids, never emails |
| `internal/csvsafe` | quotes CSV cells a spreadsheet would read as formulas |
| `internal/hosting` | Google Cloud: the fixed resource names, Secret Manager reads and writes (`config push`, hosted keys), the schedule's cron form, and the reads behind the `hosting` check |
| `internal/fakes/gcp` | an in-memory fake of Secret Manager, Cloud Run, Cloud Scheduler and Artifact Registry for tests |
| `adapters/csv` | the CSV file source (`type: csv`): lead rows, or event rows with `events: true` |
| `adapters/sheetsource` | the Google Sheet tab source (`type: sheetsource`): tabs of the team's spreadsheet |
| `adapters/apollo` | the Apollo client, enrichment, sequences sink, opt-out lookup and reply poller |
| `adapters/hubspot` | the HubSpot sink (`hubspot:contacts`, `hubspot:deals`), the opt-out and deal lookup, `setup hubspot` and the `hubspot` check |
| `internal/fakes/hubspot`, `internal/fakes/apollo` | fake vendor APIs for tests, held to the fixtures in `testdata/vendors` |
| `storetest/`, `sinktest/` | conformance suites for plug-in stores and sinks |
| `examples/` | made-up example rubrics (`rubric.yml`, and `rubric.csv-only.yml` with export lanes only), a sample lead sheet (`leads.csv`), and example `leadscore.yml` files for CSV only, Docker on a laptop and on a server, and Google Cloud |
| `compose.yaml`, `Dockerfile` | the Docker setup: the image and the compose file that runs it |
| `setup/apollo/` | the Apollo workflow templates the receiver accepts |
| `setup/gcp.sh` | the Google Cloud setup script, one subcommand per runbook step |
| `SKILL.md` | for agents running an install: the rule-change loop and one troubleshooting entry per doctor check |
| `docs/setup.md` | the setup runbook for each path |
| `docs/reference.md` | the exact formats: rubric, `leadscore.yml`, store tables, receiver, doctor checks, hosting, plug-in interfaces |
| `docs/postgres-store.md` | how to write a Postgres store as a plug-in |
| `docs/adr/` | the decision records behind the receiver, the two-phase saves and the push ledger |

## Adding an adapter or a store

leadscore takes plug-ins: sources, enrichers, pollers, lookups, sinks, detectors and stores.
`docs/reference.md`, "Plug-ins", has the interfaces. `docs/postgres-store.md` walks through a
store.

1. Put the adapter in its own package under `adapters/` and register it in `init` with the
   matching `api.Register*` function. Inside this module only `cmd/` and `_test` files import
   the root package; everything else uses `internal/api`, and `imports_test.go` holds that. An
   adapter outside this repo uses `leadscore.Register*` instead.
2. Import a built-in adapter for its side effect in `cmd/leadscore/main.go`.
3. A sink passes `sinktest.Run`. Point the sink at a fake of its vendor that implements
   `sinktest.Vendor`, and list the destinations in `sinktest.Harness`. See
   `adapters/hubspot/sink_test.go`.
4. A store passes `storetest.Run`, from an external test package. See
   `internal/store/sqlite/conformance_test.go`.
5. Tests use a fake vendor, never a real key. A live check goes behind its own variable, as
   above.

## Pull requests

- **Tests:** every behaviour change comes with a test that fails before it and passes after.
- **Safety:** never push a person known to have opted out, never cold-contact a person twice,
  never acknowledge a webhook before it is stored. A change near these carries its tests.
- **Logs:** logs carry ids, not emails. Pass log text through `internal/logredact`.
- **Changelog:** add a line under `Unreleased` in `CHANGELOG.md` for anything a user would
  notice. Tests, CI and docs changes need none.
- **Commits:** a short imperative subject (`Retry Sheets 429 with backoff`), and a body that
  says why when it is not obvious.

## Licensing of contributions

No sign-off line and no separately signed agreement are needed. By opening a pull request you
agree that your contribution is released under the project's [LICENSE](LICENSE).
