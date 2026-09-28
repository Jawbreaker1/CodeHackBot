"""Render a saved Markdown report as a local, self-contained PDF."""

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
body = renderer.render(sys.stdin.read())
document = """<!doctype html><html><head><meta charset="utf-8"><style>
    @page { size: A4; margin: 19mm 18mm 20mm; }
    body { font: 10.5pt/1.5 sans-serif; color: #18202b; }
    h1, h2, h3, h4 { color: #111827; line-height: 1.2; break-after: avoid; }
    h1 { font-size: 20pt; margin: 0 0 16pt; }
    h2 { font-size: 15pt; margin: 22pt 0 8pt; border-bottom: 1px solid #ccd2dc; padding-bottom: 4pt; }
    h3 { font-size: 12pt; margin: 17pt 0 7pt; }
    p, li { orphans: 2; widows: 2; }
    li { margin: 3pt 0; }
    table { border-collapse: collapse; width: 100%; font-size: 9pt; }
    th, td { border-bottom: 1px solid #dce1e7; padding: 5pt; text-align: left; vertical-align: top; }
    th { background: #eef1f5; }
    pre, code { font: 8.5pt monospace; overflow-wrap: anywhere; }
    pre { background: #f3f5f7; padding: 8pt; white-space: pre-wrap; }
    a { color: #9a1830; text-decoration: none; overflow-wrap: anywhere; }
    </style></head><body>""" + body + "</body></html>"

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
