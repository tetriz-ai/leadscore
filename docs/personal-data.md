# Operating leadscore with personal data

Leadscore processes contact details and activity supplied by the operator and connected
services. This runbook describes the software's behavior and operational steps; it is not a
claim of GDPR compliance. The operator must decide which data may be collected and used,
what notices and permissions are needed, who may access it, how long to retain it and how to
handle requests under the rules that apply to their use. Do that before enabling live sources,
enrichment or pushes. The software does not make those decisions or verify consent.

## Where data lives

Inventory every configured source, store, export destination, optional view and vendor account.
The authoritative columns are in [Store tables](reference.md#store-tables) and
[`internal/model/tables.go`](../internal/model/tables.go). Custom source columns, JSON fields
and plug-in stores may contain additional personal data.

| Surface | Data to include in an access or erasure review |
|---|---|
| CSV files and source Google Sheet tabs | Original contact details, supplied facts, event imports and any operator-added columns. Sources may be separate spreadsheets from the store. |
| `People`, `Identities` | Emails, LinkedIn URLs, names and other merged fields; previous conflicting values, source provenance, and permanent `merged_into` links. Include every identity and merged lead ID for the person. |
| `Company facts`, `Companies` | Current and previous facts, rollups and operator-supplied values. Check for personal data in free text or sole-trader details; do not assume every domain identifies only an organisation. |
| `Events` (SQLite), all `Events YYYY-MM` tabs (Sheets) | Raw receiver bodies, including fields that do not appear in Ranked. Inspect structured payloads, not only the current primary email. |
| `Window events`, `Seen events` | Parsed event subjects, lead IDs, attributes and de-duplication keys. Keys may encode identifiers; they are not an erasure boundary. |
| `Applied rows`, `Applied overrides`, `Overrides` | Source row IDs, hashes, lead references, email/URL selectors, merge targets and operator notes/attribution. Hashing a row does not remove its association with a person. |
| `Outcomes`, `Pushes` | Opt-outs, replies, contact/deal status, vendor IDs and the durable record of calls already made. |
| `Ranked`, every `Export <lane id>` table | Contact details, scores, derived columns, explanations and current contact restrictions. Include export lanes removed from the rubric. |
| `Log`, `Health`, `State` | IDs, operational history, errors, cursors and bookkeeping. Normal application logs redact emails and secrets; these surfaces still need review for identifiers, custom values and historical data. |
| Export files and Sheet views | SQLite writes `<export.dir>/<lane id>.csv`. An optional `store.view_spreadsheet` copies Ranked, Health and export tabs. Include old lane files/tabs, downloaded lists and copies shared with others. |
| Services and secondary copies | Apollo, HubSpot, Google Sheets/Drive, cloud or Docker volumes, backups/snapshots, local copies and external log collection. Plug-ins add their own destinations and retention. |

Apollo enrichment/lookups and HubSpot lookups receive configured identifiers or domains;
pushes send data to their configured destinations. Removing a local row does not retract a
previous push or erase the vendor's contact, sequence membership, deal or activity history.
Use each vendor's account controls and request process for those records. Review vendor
terms, access permissions and retention before connecting an account.

## Retention actually implemented

Retention is applied during runs that reach and save the relevant steps. It is not a timer
that deletes data while the service is stopped, and failures can delay cleanup.

| Data | Built-in behavior |
|---|---|
| `Log` | `log_retention`, default `90d`; configurable in `leadscore.yml`. |
| `Window events` | 90 days by event time (`at`). |
| `Seen events` | 365 days by `first_received_at`. |
| Raw SQLite `events` | Processed rows at or below the committed cursor are deleted once their received time is older than 90 days. Unprocessed rows remain. |
| Raw Sheets events | Whole processed monthly tabs are deleted after the month ends before the 90-day cutoff and the one-hour append grace has passed. The newest events tab is always kept. This can retain rows longer than 90 days. |
| People, identities, outcomes, push history and other tables | No general age-based expiry. Outcomes retain opt-outs, replies and contact/deal facts indefinitely; export rows persist, including removed lanes. Ranked is a current view. |
| Source files, exports, Sheet views, vendors, backups and external logs | No comprehensive cleanup by leadscore. The operator manages these separately. |

See [`internal/engine/intake.go`](../internal/engine/intake.go),
[`internal/engine/run.go`](../internal/engine/run.go) and the
[Sheets event store](../internal/store/sheets/events.go) for the retention implementation.
Shortening `log_retention` alone does not expire a person's profile or raw webhook data.

## Stop contacting someone

For a known email, LinkedIn URL or lead ID, use the configured installation:

```sh
leadscore set-status fake-person@example.com unsubscribed
leadscore run
leadscore explain fake-person@example.com
```

The example address is synthetic; substitute the verified person's identifier. A status
command writes an Overrides row; a subsequent successful run applies it and refreshes export
restrictions. Check the result and every associated identity/merged lead. Stop sends from
previously downloaded lists and set the corresponding opt-out in upstream senders and vendors.
Consumers of export lists must honor `do_not_contact`; a downloaded copy cannot refresh itself.
See [Statuses](reference.md#statuses) and [Opt-outs](../README.md#opt-outs) for precedence and
vendor-delivery limits.

An opt-out retains data to prevent future contact. It is not erasure. Deleting the input row
alone neither removes the existing person nor preserves suppression if the store is reset.

## Access, correction and erasure requests

Verify the request and the person's scope through your organisation's process. Use the
inventory above to find every email, canonical LinkedIn URL, lead ID, merged predecessor and
vendor ID. `leadscore explain` helps inspect the current scoring decision; it is not a complete
data export. Gather relevant rows and raw payloads separately, review for other people's data,
and transfer any response through your approved private channel. Remove temporary extracts
when finished. Correct upstream sources and operator-owned tables first; existing merged
facts may need separate review because an absent source row does not erase stored fields.

There is **no per-person erase command or supported complete per-person DELETE recipe**.
Deleting `People` alone leaves identities, event bodies, exports and durable outcomes. Deleting
Sheets event rows moves cursor positions and can stop intake with `events_shrank`. A raw text
search can miss aliases, JSON fields and merged IDs. Do not report a complete erasure based on
any of those shortcuts. If retaining other leads is required, keep affected processing stopped
and arrange a reviewed store-specific migration covering every surface before claiming success.

### Whole-workspace erasure and reset

This is the available operational fallback when the operator authorises erasing **all** leads
and history in that workspace. It also deletes suppression and already-contacted history;
rebuilding can otherwise contact people again. It does not erase upstream vendors or provider
backups. Perform the steps in this order:

1. Disable scheduled runs, stop every receiver and running job, and stop webhook delivery at
   its source. On the supplied Docker Compose installation, `docker compose stop leadscore`
   stops that service; also stop any separate CLI processes or schedulers. On Google Cloud,
   pause the scheduler and disable the receiver before deleting its Sheet. Wait for running
   jobs to finish. Keep outbound sending paused throughout the reset.
2. Remove or exclude the person's records from every CSV, source tab, pending event import,
   upstream lead feed and vendor delivery path. Arrange vendor erasure and stop any outstanding
   campaigns separately. Establish how future imports will exclude them before resuming.
3. For **SQLite**, resolve the actual `store.path` in the configuration (paths are relative to
   its config file); with all processes stopped, delete that database and any adjacent `-wal`,
   `-shm` or `-journal` files belonging to it. A named Docker data volume is separate from the
   container: deleting the container does not delete the database. Remove only the verified
   workspace data or dedicated volume; preserve unrelated volumes. This destroys every table,
   including events, cursors, outcomes and call history. File deletion is logical removal;
   handle disks, snapshots and storage-media sanitisation under your storage policy.
4. For a **Sheets store**, delete the entire dedicated store spreadsheet through its owner's
   Drive account, then remove it from trash through the applicable account controls. This
   includes hidden/protected machine tabs, monthly events, sources held in that spreadsheet,
   exports and cursors. Do not just clear visible Ranked/Leads rows. Address organisation
   retention, shared copies and provider-held history through the account administrator;
   leadscore cannot verify their erasure. Use a new spreadsheet if restarting.
5. Delete every workspace export CSV, including lanes removed from the rubric, optional Sheet
   view, downloaded/exported copy and temporary extract. Delete or restrict retained backup
   copies according to the request and your approved retention policy; prevent an old backup
   from restoring erased data. Coordinate external recipients and log administrators. A new
   run does not clean up all old files or previously copied tabs.
6. Verify that the old database/sidecars or store spreadsheet, views and exports are absent
   from active storage, and record outstanding vendor/backup actions with their owners. Record
   completion without copying contact details or raw payloads into an ordinary ticket or log.
7. If restarting, provision a fresh store using [Setup](setup.md), with `pushes_enabled: false`.
   Rebuild only from reviewed sources that exclude erased records. Re-establish opt-outs and
   prior-contact restrictions for all remaining leads before any sending resumes. Inspect
   Ranked, export restrictions and Health after a run, and confirm the erased identities do
   not return. Enable pushes only after that review.

If any upstream source, recipient, backup or vendor copy remains unresolved, document that
scope explicitly. Resetting leadscore does not establish erasure across those other systems.
