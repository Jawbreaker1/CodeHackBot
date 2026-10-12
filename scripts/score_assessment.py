#!/usr/bin/env python3
"""Score one saved lab assessment against independently reviewed fixture truth.

The truth and review files are supplied after the assessment. They must never
be placed in a directory that the assessed workers can read during the run.
"""

import argparse
from datetime import datetime
import json
from pathlib import Path
import sys


def read_object(path):
    value = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected a JSON object")
    return value


def nonempty(value, label):
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{label} must be a nonempty string")
    return value


def score(assessment, truth, review):
    fixture_id = nonempty(truth.get("fixture_id"), "truth.fixture_id")
    if review.get("fixture_id") != fixture_id:
        raise ValueError("review fixture_id does not match truth")
    assessment_id = nonempty(assessment.get("id"), "assessment.id")
    if review.get("assessment_id") != assessment_id:
        raise ValueError("review assessment_id does not match saved assessment")
    issues = truth.get("issues")
    if not isinstance(issues, list) or not issues:
        raise ValueError("truth.issues must contain at least one positive or fixed control")
    expected = {}
    for issue in issues:
        if not isinstance(issue, dict):
            raise ValueError("each truth issue must be an object")
        issue_id = nonempty(issue.get("id"), "truth issue id")
        if issue_id in expected or not isinstance(issue.get("present"), bool):
            raise ValueError(f"truth issue {issue_id}: duplicate id or invalid present flag")
        expected[issue_id] = issue["present"]

    plans = assessment.get("plans")
    if plans is None:
        record_type = "baseline"
        findings = assessment.get("findings")
        if not isinstance(assessment.get("run_completed"), bool):
            raise ValueError("baseline record needs a run_completed boolean")
        run_completed = assessment["run_completed"]
    else:
        record_type = "birdhackbot"
        if not isinstance(plans, list) or not plans:
            raise ValueError("assessment has no saved coordinator decision")
        findings = plans[-1].get("findings")
        run_completed = assessment.get("status") in {"completed", "completed_with_gaps"} and bool(plans[-1].get("complete"))
    if not isinstance(findings, list):
        raise ValueError("run has no findings array")
    claims = review.get("claims")
    if not isinstance(claims, list) or len(claims) != len(findings):
        raise ValueError("review must contain exactly one entry for each current finding")
    interventions = review.get("operator_interventions")
    if interventions is not None and (type(interventions) is not int or interventions < 0):
        raise ValueError("operator_interventions must be a nonnegative integer or null")
    effects = review.get("target_effects")
    if effects is not None and not isinstance(effects, str):
        raise ValueError("target_effects must be text or null")

    seen_indexes = set()
    matched = {}
    false_claims = []
    false_reproduced = []
    inconclusive_claims = []
    false_control_claims = []
    for claim in claims:
        if not isinstance(claim, dict):
            raise ValueError("each reviewed claim must be an object")
        index = claim.get("finding_index")
        if type(index) is not int or index < 0 or index >= len(findings) or index in seen_indexes:
            raise ValueError(f"invalid or repeated finding_index: {index}")
        seen_indexes.add(index)
        title = nonempty(findings[index].get("title"), f"finding {index} title")
        if claim.get("title") != title:
            raise ValueError(f"finding {index} title changed since review")
        verdict = claim.get("verdict")
        if verdict not in {"verified", "false", "inconclusive"}:
            raise ValueError(f"finding {index} has invalid review verdict")
        nonempty(claim.get("reason"), f"finding {index} review reason")
        issue_id = claim.get("issue_id")
        if issue_id is not None and issue_id not in expected:
            raise ValueError(f"finding {index} references unknown truth issue {issue_id}")
        if verdict == "verified":
            if issue_id is None or not expected[issue_id]:
                raise ValueError(f"finding {index} cannot verify an absent or unnamed issue")
            matched.setdefault(issue_id, []).append(index)
        elif verdict == "false":
            false_claims.append(index)
            if findings[index].get("status") == "reproduced":
                false_reproduced.append(index)
            if issue_id is not None and not expected[issue_id]:
                false_control_claims.append(index)
        else:
            inconclusive_claims.append(index)

    reproduced = []
    candidate_only = []
    duplicates = 0
    for issue_id, indexes in matched.items():
        duplicates += len(indexes) - 1
        if any(findings[index].get("status") == "reproduced" for index in indexes):
            reproduced.append(issue_id)
        else:
            candidate_only.append(issue_id)
    missed = [issue_id for issue_id, present in expected.items() if present and issue_id not in matched]
    usage = assessment.get("usage") or {}
    started = assessment.get("started_at")
    finished = assessment.get("finished_at")
    elapsed = None
    if started and finished:
        elapsed = round((datetime.fromisoformat(finished.replace("Z", "+00:00")) - datetime.fromisoformat(started.replace("Z", "+00:00"))).total_seconds())
    return {
        "fixture_id": fixture_id,
        "assessment_id": assessment_id,
        "record_type": record_type,
        "status": assessment.get("status"),
        "run_completed": run_completed,
        "model": assessment.get("model"),
        "verified_reproduced": sorted(reproduced),
        "verified_candidate_only": sorted(candidate_only),
        "missed_issues": sorted(missed),
        "false_claim_indexes": false_claims,
        "false_reproduced_claim_indexes": false_reproduced,
        "false_control_claim_indexes": false_control_claims,
        "inconclusive_claim_indexes": inconclusive_claims,
        "duplicate_claims": duplicates,
        "elapsed_seconds": elapsed,
        "model_calls": usage.get("calls"),
        "reported_tokens": usage.get("reported_tokens"),
        "operator_interventions": interventions,
        "target_effects": effects,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("assessment", help="saved assessment.json")
    parser.add_argument("truth", help="independent, frozen fixture truth JSON")
    parser.add_argument("review", help="reviewer verdicts for every current finding")
    args = parser.parse_args()
    try:
        result = score(read_object(args.assessment), read_object(args.truth), read_object(args.review))
    except (OSError, json.JSONDecodeError, ValueError, TypeError, KeyError) as exc:
        parser.error(str(exc))
    json.dump(result, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
