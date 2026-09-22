#!/usr/bin/env python3
"""Exercise the built browser API with the same deterministic model fixture as the CLI.

The fixture executes only printf in a temporary repository. This checks the actual
web binary, HTTP lifecycle, approval route, report route, and customer aggregation.
"""
import json
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from check_guided_app import Model


def call(base, path, method="GET", value=None):
    data = None if value is None else json.dumps(value).encode()
    request = Request(base + path, data=data, method=method)
    if data is not None:
        request.add_header("Content-Type", "application/json")
    try:
        with urlopen(request, timeout=5) as response:
            body = response.read().decode()
            return response.status, json.loads(body) if body else None
    except HTTPError as error:
        body = error.read().decode(errors="replace")
        raise AssertionError(f"{method} {path}: HTTP {error.code}: {body}") from error


def run_session(base, customer, goal, mode="per_action"):
    expect_approval = mode != "full_access"
    status, intake = call(base, "/api/v1/intake")
    assert status == 200 and intake["status"] == "conversation", intake
    if mode != "per_action":
        _, intake = call(base, f"/api/v1/intake/{intake['id']}/permissions", "POST", {"mode": mode, "acknowledge": True})
    _, intake = call(base, f"/api/v1/intake/{intake['id']}/messages", "POST", {"text": goal})
    assert intake["status"] == "ready" and intake["proposal"], intake
    status, view = call(base, f"/api/v1/intake/{intake['id']}/start", "POST", {"customer": customer})
    assert status == 201 and view["customer"] == customer, view
    assert view["permission_mode"] == mode, view
    assessment_id = view["id"]
    _, chat = call(base, f"/api/v1/assessments/{assessment_id}/messages", "POST", {"text": "What is the worker doing right now?"})
    assert chat["messages"][-1]["role"] == "assistant" and "waiting" in chat["messages"][-1]["text"], chat
    deadline = time.monotonic() + 10
    approved = False
    plan_approved = False
    while time.monotonic() < deadline:
        _, view = call(base, f"/api/v1/assessments/{assessment_id}")
        if view.get("pending_plan") and not plan_approved:
            plan = view["pending_plan"]
            task_ids = [task["id"] for task in plan.get("tasks", [])]
            assert task_ids, plan
            call(base, f"/api/v1/assessments/{assessment_id}/plans/{plan['id']}", "POST", {"decision": "approved", "approved_task_ids": task_ids})
            plan_approved = True
        if view["pending_approvals"] and not approved:
            approval_id = view["pending_approvals"][0]["id"]
            call(base, f"/api/v1/assessments/{assessment_id}/approvals/{approval_id}", "POST", {"decision": "approved_once"})
            approved = True
        if view["status"] in ("completed", "incomplete", "aborted") and not view["model_busy"]:
            break
        time.sleep(0.05)
    assert view["status"] == "completed" and approved == expect_approval and len(view["results"]) == 1, view
    assert view["context_window"]["limit_bytes"] > 0 and view["context_window"]["used_bytes"] > 0, view
    assert any(worker.get("context_used_bytes", 0) > 0 for worker in view["workers"]), view
    status, report = call_text(base, view["report_url"])
    assert status == 200 and "web fixture" in report, report
    return view


def call_text(base, path):
    request = Request(base + path)
    try:
        with urlopen(request, timeout=5) as response:
            return response.status, response.read().decode()
    except HTTPError as error:
        raise AssertionError(f"GET {path}: HTTP {error.code}") from error


def main():
    binary = str(Path(sys.argv[1]).resolve())
    model = ThreadingHTTPServer(("127.0.0.1", 0), Model)
    model_thread = threading.Thread(target=model.serve_forever, daemon=True)
    model_thread.start()
    process = None
    try:
        with tempfile.TemporaryDirectory(prefix="birdhackbot-web-") as temporary:
            root = Path(temporary)
            (root / "AGENTS.md").write_text("Authorized synthetic fixture commands only.\n")
            (root / "go.mod").write_text("module web-fixture\n")
            model_url = f"http://127.0.0.1:{model.server_port}/v1"
            probe = socket.socket()
            probe.bind(("127.0.0.1", 0))
            web_port = probe.getsockname()[1]
            probe.close()
            process = subprocess.Popen([
                binary,
                "-addr", f"127.0.0.1:{web_port}",
                "-repo-root", str(root),
                "-sessions-dir", str(root / "sessions"),
                "-llm-base-url", model_url,
                "-llm-model", "terminal-fixture",
                "-reasoning-effort", "low",
            ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            base = None
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                line = process.stdout.readline() if process.stdout else ""
                if line.startswith("BirdHackBot web UI: "):
                    base = line.strip().split("BirdHackBot web UI: ", 1)[1]
                    break
                if process.poll() is not None:
                    raise AssertionError(process.stderr.read() if process.stderr else "web process exited")
            if base is None:
                raise AssertionError("web binary did not announce a listen address")
            for _ in range(100):
                try:
                    status, health = call(base, "/api/v1/healthz")
                    if status == 200 and health["model_configured"]:
                        break
                except (URLError, ConnectionError):
                    time.sleep(0.05)
            else:
                raise AssertionError("web health check did not become ready")
            first = run_session(base, "web-customer", "record the first web fixture")
            second = run_session(base, "web-customer", "record the second web fixture")
            run_session(base, "permissions", "record automatic web fixture", "full_access")
            run_session(base, "permissions", "record unclassified web fixture", "dangerous_only")
            _, customer = call(base, "/api/v1/customers/web-customer")
            assert len(customer["sessions"]) == 2, customer
            assert {session["id"] for session in customer["sessions"]} == {first["id"], second["id"]}, customer
            status, report = call_text(base, customer["report_url"])
            assert status == 200 and first["id"] in report and second["id"] in report, report
            status, _ = call(base, f"/api/v1/assessments/{first['id']}", "DELETE")
            assert status == 204
            assert not (root / "sessions" / "web-customer" / first["id"]).exists()
            _, customer = call(base, "/api/v1/customers/web-customer")
            assert [session["id"] for session in customer["sessions"]] == [second["id"]], customer
            _, report = call_text(base, customer["report_url"])
            assert first["id"] not in report and second["id"] in report, report
            _, draft = call(base, "/api/v1/intake")
            status, _ = call(base, f"/api/v1/intake/{draft['id']}", "DELETE")
            assert status == 204
            assert not (root / "sessions" / "intake" / draft["id"]).exists()
            print("web application: HTTP lifecycle, approval modes, customer aggregation, and session deletion passed", flush=True)
    finally:
        if process is not None and process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        model.shutdown()
        model.server_close()


if __name__ == "__main__":
    main()
