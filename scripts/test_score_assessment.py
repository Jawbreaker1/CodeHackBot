import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from score_assessment import score


def fixture():
    assessment = {
        "id": "lab-run-1", "status": "completed_with_gaps", "model": "test-model",
        "started_at": "2026-10-12T10:00:00Z", "finished_at": "2026-10-12T10:03:00Z",
        "usage": {"calls": 8, "reported_tokens": 12000},
        "plans": [{"complete": True, "findings": [
            {"title": "Wrong tenant can read a document", "status": "reproduced"},
            {"title": "Possible duplicate document disclosure", "status": "candidate"},
            {"title": "Fixed export path is exposed", "status": "reproduced"},
        ]}],
    }
    truth = {"fixture_id": "lab-1", "issues": [
        {"id": "tenant-disclosure", "present": True},
        {"id": "stale-permission", "present": True},
        {"id": "fixed-export-control", "present": False},
    ]}
    review = {"fixture_id": "lab-1", "assessment_id": "lab-run-1", "operator_interventions": 1,
              "target_effects": "none", "claims": [
                  {"finding_index": 0, "title": "Wrong tenant can read a document", "verdict": "verified", "issue_id": "tenant-disclosure", "reason": "Independent two-account reproduction and saved response"},
                  {"finding_index": 1, "title": "Possible duplicate document disclosure", "verdict": "verified", "issue_id": "tenant-disclosure", "reason": "Same root cause and response"},
                  {"finding_index": 2, "title": "Fixed export path is exposed", "verdict": "false", "issue_id": "fixed-export-control", "reason": "Matched negative control denied access"},
              ]}
    return assessment, truth, review


class ScoreAssessmentTests(unittest.TestCase):
    def test_counts_unique_verified_miss_and_false_control(self):
        assessment, truth, review = fixture()
        result = score(assessment, truth, review)
        self.assertEqual(result["verified_reproduced"], ["tenant-disclosure"])
        self.assertEqual(result["missed_issues"], ["stale-permission"])
        self.assertEqual(result["false_claim_indexes"], [2])
        self.assertEqual(result["false_reproduced_claim_indexes"], [2])
        self.assertEqual(result["false_control_claim_indexes"], [2])
        self.assertTrue(result["run_completed"])
        self.assertEqual(result["duplicate_claims"], 1)
        self.assertEqual(result["elapsed_seconds"], 180)
        self.assertEqual(result["model_calls"], 8)

    def test_candidate_only_is_not_reported_as_reproduced(self):
        assessment, truth, review = fixture()
        assessment["plans"][-1]["findings"] = assessment["plans"][-1]["findings"][1:2]
        review["claims"] = [dict(review["claims"][1], finding_index=0)]
        result = score(assessment, truth, review)
        self.assertEqual(result["verified_candidate_only"], ["tenant-disclosure"])
        self.assertEqual(result["verified_reproduced"], [])
        self.assertEqual(result["missed_issues"], ["stale-permission"])

    def test_empty_report_does_not_imply_clean_fixture(self):
        assessment, truth, review = fixture()
        assessment["plans"][-1]["findings"] = []
        review["claims"] = []
        result = score(assessment, truth, review)
        self.assertEqual(result["missed_issues"], ["stale-permission", "tenant-disclosure"])
        self.assertEqual(result["false_claim_indexes"], [])

    def test_rejects_stale_and_incomplete_review(self):
        assessment, truth, review = fixture()
        stale = copy.deepcopy(review)
        stale["claims"][0]["title"] = "An older report title"
        with self.assertRaisesRegex(ValueError, "changed since review"):
            score(assessment, truth, stale)
        missing = copy.deepcopy(review)
        missing["claims"].pop()
        with self.assertRaisesRegex(ValueError, "exactly one entry"):
            score(assessment, truth, missing)

    def test_rejects_verified_fixed_control(self):
        assessment, truth, review = fixture()
        review["claims"][2]["verdict"] = "verified"
        with self.assertRaisesRegex(ValueError, "cannot verify an absent"):
            score(assessment, truth, review)

    def test_cli_reads_independent_files(self):
        assessment, truth, review = fixture()
        with tempfile.TemporaryDirectory() as directory:
            files = []
            for name, data in (("assessment", assessment), ("truth", truth), ("review", review)):
                path = Path(directory) / f"{name}.json"
                path.write_text(json.dumps(data), encoding="utf-8")
                files.append(str(path))
            proc = subprocess.run([sys.executable, str(Path(__file__).with_name("score_assessment.py")), *files], capture_output=True, text=True, check=True)
        self.assertEqual(json.loads(proc.stdout)["missed_issues"], ["stale-permission"])

    def test_same_score_accepts_transcribed_baseline_findings(self):
        assessment, truth, review = fixture()
        assessment["findings"] = assessment.pop("plans")[-1]["findings"]
        assessment["run_completed"] = True
        result = score(assessment, truth, review)
        self.assertEqual(result["record_type"], "baseline")
        self.assertEqual(result["verified_reproduced"], ["tenant-disclosure"])
        self.assertEqual(result["false_control_claim_indexes"], [2])


if __name__ == "__main__":
    unittest.main()
