"""Render the saved Markdown report as a self-contained, print-ready PDF."""

import json
import subprocess
import sys
import tempfile
from html import escape
from pathlib import Path

from markdown_it import MarkdownIt


def image_as_alt_text(tokens, index, options, env):
    # Report text must not cause Chromium to load local or remote image resources.
    return escape(tokens[index].content)


renderer = MarkdownIt("commonmark", {"html": False}).enable("table")
renderer.add_render_rule("image", image_as_alt_text)
metadata = json.loads(sys.argv[2])
tokens = renderer.parse(sys.stdin.read())
if tokens and tokens[0].type == "heading_open" and tokens[0].tag == "h1":
    tokens = tokens[3:]  # The report title belongs on the cover.

contents = []
for index, token in enumerate(tokens):
    if token.type == "heading_open" and token.tag in ("h2", "h3"):
        anchor = f"section-{len(contents) + 1}"
        token.attrSet("id", anchor)
        contents.append((token.tag, anchor, tokens[index + 1].content))

format_label = {
    "owasp-wstg": "OWASP WSTG-aligned",
    "ptes": "PTES-aligned",
}.get(metadata["format"], "Security assessment")
cover = f"""<section class="report-cover">
  <div class="cover-brand">BirdHackBot<span>.</span></div>
  <div class="cover-title"><p class="eyebrow">SECURITY ASSESSMENT</p>
    <h1>Security assessment<br>report</h1>
    <p class="cover-format">{escape(format_label)} · Prepared for professional review</p></div>
  <div class="cover-record"><span>Assessment</span><strong>{escape(metadata['assessment'])}</strong>
    <span>Exported</span><strong>{escape(metadata['exported'])}</strong></div>
  <p class="cover-caution">Sensitive assessment material. Share according to the engagement terms.</p>
</section>"""
contents_html = "".join(
    f'<li class="{tag}"><a href="#{anchor}">{escape(label)}</a></li>'
    for tag, anchor, label in contents
)
contents_page = f"""<section class="report-contents">
  <p class="eyebrow">REPORT GUIDE</p><h2>Contents</h2>
  <p>Start with the summary and findings. Method, limits and record checks explain what the assessment did and did not establish.</p>
  <ol>{contents_html}</ol>
</section>"""
body = renderer.renderer.render(tokens, renderer.options, {})
document = """<!doctype html><html><head><meta charset="utf-8"><style>
    @page { size: A4; margin: 19mm 18mm 20mm;
      @bottom-left { content: "BirdHackBot · Assessment report"; color: #687381; font: 8pt Arial, sans-serif; }
      @bottom-right { content: counter(page); color: #687381; font: 8pt Arial, sans-serif; }
    }
    body { font: 10pt/1.52 Arial, sans-serif; color: #1c2734; }
    .eyebrow { color: #aa2237; font: bold 9pt Arial, sans-serif; letter-spacing: 1.7pt; }
    .report-cover { box-sizing: border-box; min-height: 245mm; border-top: 5mm solid #a92339;
      padding-top: 15mm; break-after: page; display: flex; flex-direction: column; }
    .cover-brand { font-size: 17pt; font-weight: 800; letter-spacing: -.6pt; }
    .cover-brand span { color: #b51f36; }
    .cover-title { margin-top: 51mm; }
    .cover-title h1 { font-size: 36pt; line-height: 1.07; letter-spacing: -1.5pt; margin: 10pt 0 14pt; }
    .cover-format { color: #596675; font-size: 12pt; }
    .cover-record { display: grid; grid-template-columns: 27mm auto; column-gap: 4mm;
      row-gap: 4mm; border-top: 1px solid #cdd5dd; padding-top: 9mm; margin-top: auto; overflow-wrap: anywhere; }
    .cover-record span { color: #647180; }
    .cover-record strong { font-weight: 600; }
    .cover-caution { color: #647180; font-size: 8.5pt; margin-top: 13mm; }
    .report-contents { break-after: page; }
    .report-contents h2 { font-size: 25pt; border: 0; margin: 8pt 0 12pt; }
    .report-contents > p:not(.eyebrow) { max-width: 125mm; color: #596675; }
    .report-contents ol { list-style: none; margin: 16mm 0 0; padding: 0; }
    .report-contents li { border-bottom: 1px solid #e1e6eb; margin: 0; padding: 8pt 0; }
    .report-contents li.h3 { padding-left: 8mm; font-size: 9pt; }
    .report-contents a { color: #1c2734; }
    h1, h2, h3, h4 { color: #162332; line-height: 1.24; break-after: avoid; }
    h2 { font-size: 17pt; margin: 22pt 0 9pt; border-bottom: 1.5pt solid #a92339; padding-bottom: 6pt; }
    h3 { font-size: 12.5pt; margin: 17pt 0 7pt; }
    h4 { font-size: 11pt; margin: 18pt 0 8pt; padding-left: 8pt; border-left: 3pt solid #a92339; }
    p, li { orphans: 2; widows: 2; overflow-wrap: anywhere; }
    li { margin: 3pt 0; }
    table { border-collapse: collapse; width: 100%; font-size: 8.7pt; margin: 8pt 0 13pt; }
    tr { break-inside: avoid; }
    th, td { border-bottom: 1px solid #dce2e8; padding: 6pt 5pt; text-align: left; vertical-align: top; overflow-wrap: anywhere; }
    th { background: #edf1f5; color: #223143; font-weight: 700; }
    pre, code { font: 8.4pt/1.4 Consolas, monospace; overflow-wrap: anywhere; }
    pre { background: #f2f4f6; border-left: 2pt solid #c5cdd5; padding: 9pt; white-space: pre-wrap; }
    a { color: #9f2037; text-decoration: none; overflow-wrap: anywhere; }
    strong { color: #172332; }
    </style></head><body>""" + cover + contents_page + body + "</body></html>"

with tempfile.TemporaryDirectory(prefix="birdhackbot-report-") as temporary:
    source = Path(temporary) / "report.html"
    source.write_text(document, encoding="utf-8")
    subprocess.run(
        [
            "/usr/bin/chromium",
            "--headless",
            "--disable-gpu",
            "--disable-background-networking",
            "--disable-extensions",
            "--disable-default-apps",
            "--disable-sync",
            "--no-first-run",
            "--no-pdf-header-footer",
            "--user-data-dir=" + temporary + "/profile",
            "--print-to-pdf=" + sys.argv[1],
            source.as_uri(),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        timeout=50,
    )
