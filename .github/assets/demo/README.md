# Visual walkthrough

Four scenes show actual offline sample results: eight leads are scored; Anna's 65 is
explained as 40 company + 10 region + 15 role; increasing warm-path points from 10 to
30 moves Jonas from 60 to 80 and first place; sample export lanes have five call-list
leads, one nurture lead and two excluded leads.

These are code-drawn illustrations, not a web interface. The made-up inputs come from
`examples/leads.csv`; no real contact or CRM is involved. Export lanes are used, so
there is no live Apollo/HubSpot push and no email is sent. Review `do_not_contact`
before using an export to contact anyone.

[ranking.txt](ranking.txt), [explanation.txt](explanation.txt) and
[reranking.txt](reranking.txt) retain real offline example output. Refresh from the
repo root:

```sh
examples/score-a-csv/run.sh > .github/assets/demo/ranking.txt
examples/explain-a-lead/run.sh > .github/assets/demo/explanation.txt
examples/change-the-rubric/run.sh > .github/assets/demo/reranking.txt
python .github/assets/render-visual-demo.py --preview-dir /tmp/lead-priority-demo
```

Use Go 1.26+ for the examples and Python 3.12 with the optional
`.github/assets/requirements.txt` installed in a temporary environment for rendering.
The original [terminal recording](../terminal-demo.gif) is retained; its VHS tape now
writes `terminal-demo.gif` so it cannot overwrite the overview.

`story.json` holds the copy and evidence checks. No font or third-party artwork is
bundled. Arial or DejaVu Sans supplies the type. Pillow is separately installed under
MIT-CMU terms and is not linked into the Go binary. Inspect all keyframes and motion.
