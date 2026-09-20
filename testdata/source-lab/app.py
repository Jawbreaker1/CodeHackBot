#!/usr/bin/env python3
"""Synthetic, loopback-only tenant reporting service for assessment validation."""
import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

VERSION = "2.4.1"
ACCOUNTS = {"lab-alice": "north", "lab-bob": "south"}
REPORTS = {
    "101": {"id": "101", "tenant": "north", "body": "SYNTHETIC_NORTH_REPORT"},
    "202": {"id": "202", "tenant": "south", "body": "SYNTHETIC_SOUTH_REPORT"},
}


class Handler(BaseHTTPRequestHandler):
    server_version = "LedgerLab/" + VERSION
    sys_version = ""

    def do_GET(self):
        route = self.path.split("?", 1)[0]
        if route == "/":
            return self.reply(200, {
                "product": "LedgerLab", "version": VERSION,
                "revision": self.server.revision,
                "source_repository": self.server.repository,
                "routes": ["/api/v1/reports/{id}", "/api/v2/reports/{id}", "/debug"],
            })
        if route == "/debug":
            return self.reply(404, {"error": "debug feature disabled"})
        tenant = ACCOUNTS.get(self.headers.get("Authorization", "").removeprefix("Bearer "))
        if not tenant:
            return self.reply(401, {"error": "authentication required"})
        parts = route.strip("/").split("/")
        if len(parts) != 4 or parts[0] != "api" or parts[1] not in ("v1", "v2") or parts[2] != "reports":
            return self.reply(404, {"error": "not found"})
        report = REPORTS.get(parts[3])
        if not report:
            return self.reply(404, {"error": "report not found"})
        if parts[1] == "v2" and report["tenant"] != tenant:
            return self.reply(403, {"error": "tenant access denied"})
        return self.reply(200, report)

    def reply(self, status, body):
        encoded = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


def create_server(repository, revision):
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.repository, server.revision = repository, revision
    return server


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--ready-file", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--revision", required=True)
    args = parser.parse_args()
    server = create_server(args.repository, args.revision)
    Path(args.ready_file).write_text(f"http://127.0.0.1:{server.server_port}\n")
    server.serve_forever()
