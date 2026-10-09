"""Small source-review target with a protected document download route.

Run with: python3 app.py --port 8767
All files are synthetic. The private fixture is deliberately outside the
public document directory; no target-specific BirdHackBot logic is needed.
"""

import argparse
import html
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit


ROOT = Path(__file__).resolve().parent
PUBLIC = (ROOT / "public").resolve()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return

    def respond(self, status, body, content_type="text/plain"):
        raw = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", f"{content_type}; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        route = urlsplit(self.path)
        if route.path == "/":
            links = "".join(
                f'<li><a href="/api/files?path={html.escape(p.name, quote=True)}">'
                f"{html.escape(p.name)}</a></li>"
                for p in sorted(PUBLIC.iterdir()) if p.is_file()
            )
            self.respond(
                HTTPStatus.OK,
                "<!doctype html><meta charset=utf-8><title>Harbor documents</title>"
                "<h1>Harbor public documents</h1><p>Download a public file.</p>"
                f"<ul>{links}</ul>",
                "text/html",
            )
            return
        if route.path != "/api/files":
            self.respond(HTTPStatus.NOT_FOUND, "Not found")
            return
        names = parse_qs(route.query).get("path", [])
        if len(names) != 1 or not names[0]:
            self.respond(HTTPStatus.BAD_REQUEST, "Specify one path")
            return
        requested = (PUBLIC / names[0]).resolve()
        if not requested.is_relative_to(PUBLIC) or not requested.is_file():
            self.respond(HTTPStatus.NOT_FOUND, "Document not found")
            return
        self.respond(HTTPStatus.OK, requested.read_text(encoding="utf-8"))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8767)
    args = parser.parse_args()
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
