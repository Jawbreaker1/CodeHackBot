"""Independent behavioral oracle for the synthetic editorial fixture."""

import http.cookiejar
import importlib.util
import json
import pathlib
import threading
import unittest
import urllib.error
import urllib.parse
import urllib.request
from http import HTTPStatus
from http.server import ThreadingHTTPServer


ROOT = pathlib.Path(__file__).resolve().parents[1]
SOURCE = ROOT / "testdata/review-workflow-lab/app.py"
spec = importlib.util.spec_from_file_location("review_workflow_fixture", SOURCE)
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


class FixedControl(app.Handler):
    def do_POST(self):
        if self.path == "/publish" and self.actor() and self.actor()["role"] == "author":
            if app.document["approved_revision"] != app.document["revision"]:
                self.respond(HTTPStatus.CONFLICT, {"error": "latest revision requires review"})
                return
        super().do_POST()


def client():
    return urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )


def request(opener, base, route, data=None):
    encoded = None if data is None else urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(base + route, data=encoded)
    try:
        with opener.open(req, timeout=3) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


class ReviewWorkflowTest(unittest.TestCase):
    def exercise(self, handler, expected_publish):
        app.sessions.clear()
        app.document.update({
            "body": "The service window is scheduled for Friday.",
            "revision": 1,
            "state": "draft",
            "approved_revision": None,
            "published_body": None,
        })
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = "http://127.0.0.1:" + str(server.server_port)
        try:
            author, reviewer, stranger = client(), client(), client()
            self.assertEqual(request(stranger, base, "/api/document")[0], 401)
            self.assertEqual(request(author, base, "/login", {"user": "writer", "password": "writer-pass"})[0], 200)
            self.assertEqual(request(reviewer, base, "/login", {"user": "reviewer", "password": "reviewer-pass"})[0], 200)
            self.assertEqual(request(author, base, "/publish", {})[0], 409)
            self.assertEqual(request(author, base, "/approve", {})[0], 403)
            self.assertEqual(request(author, base, "/edit", {"body": "Approved wording"})[0], 200)
            self.assertEqual(request(author, base, "/submit", {})[0], 200)
            self.assertEqual(request(reviewer, base, "/approve", {})[0], 200)
            self.assertEqual(app.document["approved_revision"], 2)
            self.assertEqual(request(author, base, "/edit", {"body": "Unreviewed wording"})[0], 200)
            self.assertEqual(app.document["revision"], 3)
            status, result = request(author, base, "/publish", {})
            self.assertEqual(status, expected_publish)
            public_status, public = request(stranger, base, "/api/public")
            self.assertEqual(public_status, 200)
            if expected_publish == 200:
                self.assertEqual(result["published_revision"], 3)
                self.assertEqual(public["body"], "Unreviewed wording")
            else:
                self.assertIsNone(public["body"])
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)

    def test_current_app_publishes_changed_content(self):
        self.exercise(app.Handler, 200)

    def test_fixed_control_rejects_changed_content(self):
        self.exercise(FixedControl, 409)


if __name__ == "__main__":
    unittest.main()
