"""Independent HTTP oracle for the fabricated AccountDesk fixture."""

import http.cookiejar
import importlib.util
import json
import pathlib
import sys
import threading
import unittest
import urllib.error
import urllib.parse
import urllib.request
from http.server import ThreadingHTTPServer


ROOT = pathlib.Path(__file__).resolve().parents[1]
FIXTURE = ROOT / "testdata/cache-lab"
sys.path.insert(0, str(FIXTURE))
spec = importlib.util.spec_from_file_location("accountdesk_fixture", FIXTURE / "app.py")
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)
from summary import reset_cache  # noqa: E402


def client():
    return urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )


def request(opener, base, route, form=None):
    body = None if form is None else urllib.parse.urlencode(form).encode()
    req = urllib.request.Request(base + route, data=body)
    try:
        with opener.open(req, timeout=3) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


class AccountDeskOracle(unittest.TestCase):
    def test_account_isolation_and_control(self):
        reset_cache()
        server = ThreadingHTTPServer(("127.0.0.1", 0), app.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = f"http://127.0.0.1:{server.server_port}"
        try:
            cedar, maple, stranger = client(), client(), client()
            for route in ("/api/v1/account/summary", "/api/v2/account/summary"):
                self.assertEqual(request(stranger, base, route)[0], 401)
            for opener, account in ((cedar, "cedar"), (maple, "maple")):
                status, result = request(opener, base, "/login", {
                    "username": account, "password": account + "-pass",
                })
                self.assertEqual((status, result["account"]), (200, account))
                self.assertEqual(request(opener, base, "/api/me"), (200, {"account": account}))

            old = "/api/v1/account/summary"
            new = "/api/v2/account/summary"
            self.assertEqual(request(cedar, base, old)[1]["account"], "cedar")
            self.assertEqual(request(maple, base, old)[1]["note"], "SYNTHETIC_CEDAR_NOTE")
            reset_cache()
            self.assertEqual(request(maple, base, old)[1]["account"], "maple")
            self.assertEqual(request(cedar, base, old)[1]["note"], "SYNTHETIC_MAPLE_NOTE")

            reset_cache()
            self.assertEqual(request(cedar, base, new)[1]["account"], "cedar")
            self.assertEqual(request(maple, base, new)[1]["account"], "maple")
            self.assertEqual(request(cedar, base, new)[1]["note"], "SYNTHETIC_CEDAR_NOTE")
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
