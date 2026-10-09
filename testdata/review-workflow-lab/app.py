"""Small, isolated editorial workflow for assessment exercises.

Run with: python3 app.py --port 8766
All accounts and documents are synthetic. State resets on restart.
"""

import argparse
import json
import secrets
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs


USERS = {"writer": ("writer-pass", "author"), "reviewer": ("reviewer-pass", "reviewer")}
sessions = {}
document = {
    "id": 42,
    "title": "Service notice",
    "body": "The service window is scheduled for Friday.",
    "revision": 1,
    "state": "draft",
    "approved_revision": None,
    "published_body": None,
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return

    def respond(self, status, value, content_type="application/json", cookie=None):
        raw = (json.dumps(value) if content_type == "application/json" else value).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type + "; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        if cookie:
            self.send_header("Set-Cookie", cookie)
        self.end_headers()
        self.wfile.write(raw)

    def actor(self):
        for item in self.headers.get("Cookie", "").split(";"):
            key, _, value = item.strip().partition("=")
            if key == "sid":
                return sessions.get(value)
        return None

    def payload(self):
        length = int(self.headers.get("Content-Length", "0"))
        if length > 16_384:
            raise ValueError("request too large")
        raw = self.rfile.read(length).decode()
        if self.headers.get("Content-Type", "").startswith("application/json"):
            return json.loads(raw)
        return {key: values[0] for key, values in parse_qs(raw).items()}

    def do_GET(self):
        if self.path == "/api/public":
            self.respond(HTTPStatus.OK, {"title": document["title"], "body": document["published_body"]})
            return
        actor = self.actor()
        if self.path == "/api/document":
            if not actor:
                self.respond(HTTPStatus.UNAUTHORIZED, {"error": "sign in first"})
            else:
                self.respond(HTTPStatus.OK, {"actor": actor, "document": document})
            return
        if self.path != "/":
            self.respond(HTTPStatus.NOT_FOUND, {"error": "no such route"})
            return
        if not actor:
            page = """<h1>Harbor editorial desk</h1><p>Sign in to review the service notice.</p>
<form method=post action=/login><input name=user placeholder=User>
<input name=password type=password placeholder=Password><button>Sign in</button></form>"""
        else:
            import html

            role = html.escape(actor["role"])
            body = html.escape(document["body"])
            status = html.escape(document["state"])
            page = f"<h1>Harbor editorial desk</h1><p>Signed in as {role}</p><h2>Service notice</h2>"
            page += f"<p>Revision {document['revision']} · {status}</p><pre>{body}</pre>"
            if role == "author":
                page += "<form method=post action=/edit><textarea name=body rows=5 cols=70>" + body + "</textarea><button>Save edit</button></form>"
                page += "<form method=post action=/submit><button>Submit for review</button></form>"
                page += "<form method=post action=/publish><button>Publish</button></form>"
            if role == "reviewer":
                page += "<form method=post action=/approve><button>Approve</button></form>"
            page += "<p><a href=/api/document>Document API</a> · <a href=/api/public>Public copy</a></p>"
        self.respond(HTTPStatus.OK, "<!doctype html><meta charset=utf-8><title>Harbor editorial desk</title>" + page, "text/html")

    def do_POST(self):
        try:
            data = self.payload()
        except (ValueError, json.JSONDecodeError):
            self.respond(HTTPStatus.BAD_REQUEST, {"error": "invalid request"})
            return
        if self.path == "/login":
            user = data.get("user", "")
            account = USERS.get(user)
            if not account or data.get("password") != account[0]:
                self.respond(HTTPStatus.UNAUTHORIZED, {"error": "bad credentials"})
                return
            sid = secrets.token_urlsafe(24)
            actor = {"name": user, "role": account[1]}
            sessions[sid] = actor
            self.respond(HTTPStatus.OK, {"signed_in": actor}, cookie=f"sid={sid}; HttpOnly; SameSite=Lax; Path=/")
            return
        actor = self.actor()
        if not actor:
            self.respond(HTTPStatus.UNAUTHORIZED, {"error": "sign in first"})
            return
        role = actor["role"]
        if self.path in ("/edit", "/submit", "/publish") and role != "author":
            self.respond(HTTPStatus.FORBIDDEN, {"error": "authors only"})
            return
        if self.path == "/approve" and role != "reviewer":
            self.respond(HTTPStatus.FORBIDDEN, {"error": "reviewers only"})
            return
        if self.path == "/edit":
            body = data.get("body")
            if not isinstance(body, str) or not body.strip() or len(body) > 2000:
                self.respond(HTTPStatus.BAD_REQUEST, {"error": "body required, max 2000 characters"})
                return
            document["body"] = body
            document["revision"] += 1
            self.respond(HTTPStatus.OK, {"saved_revision": document["revision"]})
            return
        if self.path == "/submit":
            document["state"] = "submitted"
            self.respond(HTTPStatus.OK, {"state": document["state"]})
            return
        if self.path == "/approve":
            if document["state"] != "submitted":
                self.respond(HTTPStatus.CONFLICT, {"error": "submit before approval"})
                return
            document["approved_revision"] = document["revision"]
            document["state"] = "approved"
            self.respond(HTTPStatus.OK, {"approved_revision": document["approved_revision"]})
            return
        if self.path == "/publish":
            if document["state"] != "approved" or document["approved_revision"] is None:
                self.respond(HTTPStatus.CONFLICT, {"error": "review required"})
                return
            document["published_body"] = document["body"]
            document["state"] = "published"
            self.respond(HTTPStatus.OK, {"state": document["state"], "published_revision": document["revision"]})
            return
        self.respond(HTTPStatus.NOT_FOUND, {"error": "no such route"})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8766)
    args = parser.parse_args()
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
