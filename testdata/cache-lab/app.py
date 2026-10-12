#!/usr/bin/env python3
"""Loopback-only account service with fabricated users and records."""

import argparse
import json
import secrets
from http import HTTPStatus
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import RLock
from urllib.parse import parse_qs, urlsplit

from accounts import ACCOUNTS
from summary import account_summary


_sessions = {}
_sessions_lock = RLock()


class Handler(BaseHTTPRequestHandler):
    server_version = "AccountDesk/2.1"
    sys_version = ""

    def log_message(self, format, *args):
        return

    def respond(self, status, body, *, content_type="application/json", cookie=None):
        raw = body.encode() if isinstance(body, str) else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "private, no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        if cookie:
            self.send_header("Set-Cookie", cookie)
        self.end_headers()
        self.wfile.write(raw)

    def account(self):
        cookies = SimpleCookie()
        try:
            cookies.load(self.headers.get("Cookie", ""))
        except Exception:
            return None
        session = cookies.get("session")
        if not session:
            return None
        with _sessions_lock:
            return _sessions.get(session.value)

    def do_POST(self):
        if urlsplit(self.path).path != "/login":
            self.respond(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            size = 0
        if size < 1 or size > 2048:
            self.respond(HTTPStatus.BAD_REQUEST, {"error": "invalid form"})
            return
        fields = parse_qs(self.rfile.read(size).decode("utf-8", "replace"))
        username = fields.get("username", [""])[0]
        password = fields.get("password", [""])[0]
        if ACCOUNTS.get(username) != password:
            self.respond(HTTPStatus.UNAUTHORIZED, {"error": "invalid credentials"})
            return
        token = secrets.token_urlsafe(24)
        with _sessions_lock:
            _sessions[token] = username
        self.respond(
            HTTPStatus.OK,
            {"account": username},
            cookie=f"session={token}; HttpOnly; SameSite=Lax; Path=/",
        )

    def do_GET(self):
        route = urlsplit(self.path).path
        if route == "/":
            self.respond(
                HTTPStatus.OK,
                "<!doctype html><title>AccountDesk</title><h1>AccountDesk</h1>"
                "<p>Account summary API. Sign in with a test account to view your own data.</p>"
                "<ul><li>POST /login (username, password)</li>"
                "<li>GET /api/me</li>"
                "<li>GET /api/v1/account/summary</li>"
                "<li>GET /api/v2/account/summary</li></ul>",
                content_type="text/html; charset=utf-8",
            )
            return
        if route not in (
            "/api/me",
            "/api/v1/account/summary",
            "/api/v2/account/summary",
        ):
            self.respond(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        account = self.account()
        if account is None:
            self.respond(HTTPStatus.UNAUTHORIZED, {"error": "authentication required"})
            return
        if route == "/api/me":
            self.respond(HTTPStatus.OK, {"account": account})
            return
        summary, _ = account_summary(route, account)
        self.respond(HTTPStatus.OK, summary)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--ready-file", type=Path, required=True)
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    args.ready_file.write_text(f"http://127.0.0.1:{server.server_port}\n")
    server.serve_forever()
