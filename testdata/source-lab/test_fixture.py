import json
import subprocess
import tempfile
import threading
import unittest
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from app import create_server
from prepare import prepare


class FixtureTest(unittest.TestCase):
    def test_target_truth(self):
        server = create_server("file:///synthetic/upstream", "fixture-revision")
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join)
        self.addCleanup(server.shutdown)
        base = f"http://127.0.0.1:{server.server_port}"

        def get(path, token=None):
            request = Request(base + path, headers={"Authorization": "Bearer " + token} if token else {})
            try:
                response = urlopen(request, timeout=5)
            except HTTPError as error:
                response = error
            with response:
                return response.status, json.load(response)

        status, identity = get("/")
        self.assertEqual((status, identity["version"], identity["revision"]), (200, "2.4.1", "fixture-revision"))
        self.assertEqual(get("/debug")[0], 404)
        for own, foreign, token in [("101", "202", "lab-alice"), ("202", "101", "lab-bob")]:
            for api in ("v1", "v2"):
                self.assertEqual(get(f"/api/{api}/reports/{own}")[0], 401)
                self.assertEqual(get(f"/api/{api}/reports/{own}", token)[0], 200)
            status, report = get(f"/api/v1/reports/{foreign}", token)
            self.assertEqual((status, report["id"]), (200, foreign))
            self.assertEqual(get(f"/api/v2/reports/{foreign}", token)[0], 403)

    def test_advertised_revision_differs_from_default_source(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "fixture"
            manifest = prepare(root)
            self.assertNotEqual(manifest["deployed_revision"], manifest["default_revision"])
            deployed = subprocess.check_output(["git", "-C", str(root / "upstream"), "show", manifest["deployed_revision"] + ":app.py"], text=True)
            self.assertEqual(deployed, Path(__file__).with_name("app.py").read_text())
            self.assertIn('VERSION = "2.4.2"', (root / "upstream/app.py").read_text())


if __name__ == "__main__":
    unittest.main()
