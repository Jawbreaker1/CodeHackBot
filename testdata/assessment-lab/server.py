#!/usr/bin/env python3
"""Loopback-only assessment fixture. All contents are synthetic."""
import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


class Handler(BaseHTTPRequestHandler):
    server_version = "BirdLab/1.4"
    sys_version = ""

    def do_GET(self):
        routes = {
            "/": (200, {"product": "BirdLab", "version": "1.4", "routes": ["/admin", "/fixed/admin", "/status"]}),
            "/status": (200, {"status": "healthy", "debug": False}),
            "/admin": (200, {"role": "administrator", "configuration": "SYNTHETIC_ADMIN_CONFIGURATION"}),
            "/fixed/admin": (403, {"error": "authentication required"}),
        }
        status, body = routes.get(self.path, (404, {"error": "not found"}))
        encoded = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--ready-file", required=True)
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    Path(args.ready_file).write_text(f"http://127.0.0.1:{server.server_port}\n")
    server.serve_forever()
