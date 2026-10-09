"""Independent HTTP check of the protected local document fixture."""

import importlib.util
import pathlib
import threading
import unittest
import urllib.error
import urllib.parse
import urllib.request
from http.server import ThreadingHTTPServer


ROOT = pathlib.Path(__file__).resolve().parents[1]
SOURCE = ROOT / "testdata/safe-documents-lab/app.py"
spec = importlib.util.spec_from_file_location("safe_documents_fixture", SOURCE)
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


def get(base, route):
    try:
        with urllib.request.urlopen(base + route, timeout=3) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()


class SafeDocumentsTest(unittest.TestCase):
    def test_public_document_and_private_boundary(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), app.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = f"http://127.0.0.1:{server.server_port}"
        try:
            home_status, home = get(base, "/")
            self.assertEqual(home_status, 200)
            self.assertIn("notice.txt", home)
            public_status, public = get(base, "/api/files?path=notice.txt")
            self.assertEqual(public_status, 200)
            self.assertIn("public document service", public)
            for candidate in ("../private.txt", "../../private.txt", str(app.ROOT / "private.txt")):
                path = urllib.parse.quote(candidate, safe="")
                status, body = get(base, "/api/files?path=" + path)
                self.assertEqual(status, 404)
                self.assertNotIn("SYNTHETIC-PRIVATE-MARKER", body)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
