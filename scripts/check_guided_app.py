#!/usr/bin/env python3
"""Exercise the built application through a PTY, using a deterministic model.

No live model or target is contacted. Only printf and a cancellable sleep run.
Uses the Python standard library; invoked by ci.sh after building the binary.
"""
import errno
import json
import os
from pathlib import Path
import pty
import select
import signal
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Model(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def send_json(self, body):
        encoded = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self):
        self.send_json({"data": [{"id": "terminal-fixture"}]})

    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        assert request.get("reasoning_effort") == "low", request.keys()
        assert request.get("max_tokens") == 32768, request.keys()
        messages = request["messages"]
        if "conversational assessment orchestrator" in messages[0]["content"]:
            latest = messages[-1]["content"]
            if latest == "Who are you?":
                result = {"reply": "I am the assessment orchestrator. I can explain the harness and help define an authorized assessment before any worker runs.", "proposal": None}
            else:
                goal = "Print terminal fixture"
                for candidate in ("cancellation fixture", "question fixture", "recovery fixture", "orchestrate generic capability checks"):
                    if candidate in latest:
                        goal = candidate
                        break
                result = {"reply": "I have a proposed objective and exact scope ready for review.", "proposal": {"goal": goal, "scope": "Local synthetic commands only; no target network access"}}
            self.send_json({"choices": [{"message": {"content": json.dumps(result)}}], "usage": {"total_tokens": 10}})
            return
        if "conversational interface" in messages[0]["content"]:
            self.send_json({"choices": [{"message": {"content": "The worker is waiting for your approval before it runs the proposed action."}}], "usage": {"total_tokens": 10}})
            return
        prompt = messages[1]["content"]
        payload = json.loads(prompt)
        if "Operator answer: fixture answer" in prompt:
            remaining = 9 if payload.get("role") == "worker" else 8
            assert f"remaining_budget: {remaining} steps" in prompt, "worker budget did not decrease after the question/action"
        if payload.get("role") == "assessment_coordinator":
            state = payload["assessment"]
            tasks = []
            if not state["results"]:
                if "orchestrate" in state["goal"]:
                    tasks = [
                        {"id": "discover", "goal": "Collect generic discovery evidence from the allowed fixture", "done_when": "discovery evidence recorded", "depends_on": []},
                        {"id": "control", "goal": "Collect a separate control observation from the allowed fixture", "done_when": "control evidence recorded", "depends_on": []},
                    ]
                else:
                    tasks = [{"id": "observe", "goal": state["goal"], "done_when": "literal output recorded", "depends_on": []}]
            elif "orchestrate" in state["goal"] and len(state["results"]) == 2:
                tasks = [{"id": "validate", "goal": "Validate the discovery observation using the completed evidence", "done_when": "dependent validation evidence recorded", "depends_on": ["discover"]}]
            result = {"summary": "Generic orchestrator fixture assessment", "complete": not tasks, "tasks": tasks, "findings": [], "gaps": ["Synthetic capability fixture only"]}
        elif payload.get("role") == "goal_evaluator":
            missing = "[latest_execution_result]\naction: cat" in payload["context_packet"]
            result = {"status": "blocked" if missing else "satisfied", "reason": "Path absent; choose the alternative" if missing else "Literal output exists", "summary": "Terminal fixture observed"}
        elif "recovery fixture" in prompt:
            retry = "Goal not yet satisfied" in prompt
            step = "Use the alternative" if retry else "Read initial path"
            result = {"type": "action", "command": "printf" if retry else "cat", "args": ["%s", "terminal fixture"] if retry else ["missing-fixture.txt"], "plan": {"summary": step, "steps": [step], "active_step": step}}
        elif "question fixture" in prompt and "Operator answer: fixture answer" not in prompt:
            result = {"type": "ask_user", "question": "Which fixture value should I use?"}
        elif "cancellation fixture" in prompt:
            result = {"type": "action", "command": "sh", "args": ["-c", "printf ready > ready; sleep 30"]}
        else:
            result = {"type": "action", "command": "printf", "args": ["%s", "terminal fixture"]}
        self.send_json({"choices": [{"message": {"content": json.dumps(result)}}], "usage": {"total_tokens": 10}})


class Terminal:
    def __init__(self, binary, cwd, plain=True):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(cwd)
            environment = os.environ.copy()
            if plain:
                environment["BIRDHACKBOT_PLAIN"] = "1"
            else:
                environment.pop("BIRDHACKBOT_PLAIN", None)
            os.execve(binary, [binary], environment)
        self.plain = plain
        self.transcript = ""
        self.pending = ""
        self.reaped = False

    def read(self, timeout=0.1):
        if select.select([self.fd], [], [], timeout)[0]:
            try:
                value = os.read(self.fd, 65536).decode(errors="replace")
            except OSError as error:
                if error.errno == errno.EIO:
                    return False
                raise
            self.transcript += value
            self.pending += value
            return bool(value)
        return True

    def expect(self, marker, timeout=10):
        deadline = time.monotonic() + timeout
        while marker not in self.pending:
            if time.monotonic() > deadline or not self.read():
                raise AssertionError(f"Missing terminal output {marker!r}\n{self.transcript}")
        before, self.pending = self.pending.split(marker, 1)
        return before

    def send(self, value):
        os.write(self.fd, (value + ("\n" if self.plain else "\r")).encode())

    def interrupt(self):
        os.write(self.fd, b"\x03")

    def finish(self, expected):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            self.read()
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.reaped = True
                code = os.waitstatus_to_exitcode(status)
                assert code == expected, (code, self.transcript)
                return
        raise AssertionError("Application did not exit\n" + self.transcript)

    def close(self):
        if not self.reaped:
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)
        os.close(self.fd)


def run_case(binary, root, endpoint, mode):
    terminal = Terminal(binary, root)
    try:
        if mode == "reuse":
            terminal.expect("Use saved provider")
            terminal.send("")
        else:
            terminal.expect("Selection [1]")
            terminal.send("1")
            terminal.expect("Local model server address")
            if mode == "provider-error":
                terminal.send("not-a-server")
                terminal.expect("Cannot connect:")
                terminal.expect("Local model server address")
            terminal.send(endpoint)
            terminal.expect("Choose a model number")
            terminal.send("1")
            terminal.expect("Reasoning effort:")
            terminal.send("low")
        terminal.expect("birdhackbot> ")
        if mode == "auto":
            terminal.send("/permissions")
            terminal.expect("Choose 1–3")
            terminal.send("3")
            terminal.expect("Type confirm to apply.")
            terminal.send("confirm")
            terminal.expect("Approval setting: Approve everything")
            terminal.expect("birdhackbot> ")
        terminal.send("Who are you?")
        terminal.expect("Coordinator: I am the assessment orchestrator.")
        terminal.expect("birdhackbot> ")
        goal = {"stop": "cancellation fixture", "question": "question fixture", "recovery": "recovery fixture", "orchestration": "orchestrate generic capability checks"}.get(mode, "Print terminal fixture")
        terminal.send(goal)
        terminal.expect("Assessment review")
        terminal.expect("Type start")
        terminal.send("" if mode == "cancel" else "start")
        if mode == "cancel":
            terminal.expect("Assessment canceled before execution.")
            terminal.finish(0)
            assert not list((root / "sessions").glob("assessment-*"))
            return
        if mode == "question":
            terminal.expect("Question from observe:")
            terminal.send("fixture answer")
        if mode == "recovery":
            terminal.expect("Allow this action?")
            terminal.send("d")
            before = terminal.expect("Allow this action?")
            assert "Exact invocation: cat missing-fixture.txt" in before, terminal.transcript
            terminal.send("y")
        approvals = 0 if mode == "auto" else 3 if mode == "orchestration" else 1
        for approval_index in range(approvals):
            terminal.expect("Allow this action?")
            terminal.send("d")
            before = terminal.expect("Allow this action?")
            expected = "sh -c 'printf ready > ready; sleep 30'" if mode == "stop" else "printf '%s' 'terminal fixture'"
            if mode != "orchestration":
                assert "Exact invocation: " + expected in before, terminal.transcript
            terminal.send("n" if mode == "deny" else "y")
        if mode == "stop":
            deadline = time.monotonic() + 5
            while not list((root / "sessions").glob("assessment-*/tasks/observe/work/ready")):
                assert time.monotonic() < deadline, "child command never started"
                terminal.read()
            os.write(terminal.fd, b"\x03")
            terminal.expect("Assessment aborted.")
            terminal.finish(130)
        else:
            terminal.expect("Assessment incomplete." if mode == "deny" else "Assessment completed.")
            terminal.expect("Report:")
            terminal.finish(0)
        runs = sorted((root / "sessions").glob("assessment-*/assessment.json"), key=lambda p: p.stat().st_mtime_ns)
        state = json.loads(runs[-1].read_text())
        assert state["reasoning_effort"] == "low", state
        assert state["max_output_tokens"] == 32768, state
        assert state["results"][0]["status"] == {"deny": "failed", "stop": "aborted"}.get(mode, "done"), state
        if mode == "deny":
            assert not state["results"][0]["evidence"], state
        if mode == "recovery":
            evidence = state["results"][0]["evidence"]
            assert len(evidence) == 2 and evidence[0]["ExitStatus"] != "0" and evidence[1]["ExitStatus"] == "0", evidence
            worker = json.loads((runs[-1].parent / "tasks/observe/session.json").read_text())
            assert worker["packet"]["PlanState"]["ActiveStep"] == "Use the alternative", worker
            assert worker["packet"]["Budget"] == {"Limit": 10, "Used": 2}, worker
        if mode == "orchestration":
            assert state["status"] == "completed" and len(state["results"]) == 3, state
            assert {item["task"]["id"] for item in state["results"]} == {"discover", "control", "validate"}, state
            validate = next(item for item in state["results"] if item["task"]["id"] == "validate")
            assert validate["task"]["depends_on"] == ["discover"], validate
            assert len({item["evidence"][0]["Cwd"] for item in state["results"]}) == 3, state
            assert state["usage"]["calls"] == 9, state
            assert "Generic orchestrator fixture assessment" in (runs[-1].parent / "report.md").read_text()
        assert (runs[-1].parent / "report.md").exists()
        prefs = json.loads((root / ".birdhackbot/preferences.json").read_text())
        assert set(prefs) == {"provider", "base_url", "model", "reasoning_effort", "max_output_tokens", "max_input_bytes"}, prefs
        assert prefs["reasoning_effort"] == "low", prefs
        assert prefs["max_input_bytes"] == 48 * 1024, prefs
    finally:
        (root / f"terminal-{mode}.txt").write_text(terminal.transcript)
        terminal.close()


def run_tui_smoke(binary, root, endpoint):
    terminal = Terminal(binary, root, plain=False)
    try:
        terminal.expect("Choose model access:")
        terminal.send("1")
        terminal.expect("Local model server address")
        terminal.send(endpoint)
        terminal.expect("Choose a model number")
        terminal.send("1")
        terminal.expect("Reasoning effort:")
        terminal.send("low")
        terminal.expect("birdhackbot>")
        terminal.send("Who are you?")
        terminal.expect("Coordinator: I am the assessment orchestrator.")
        terminal.interrupt()
        terminal.finish(0)
        assert "Coordinator: I am the assessment orchestrator." in terminal.transcript
        assert "\n> " not in terminal.transcript, "raw console prompts leaked into the TUI stream"
    finally:
        (root / "terminal-tui.txt").write_text(terminal.transcript)
        terminal.close()


def main():
    binary = str(Path(sys.argv[1]).resolve())
    server = ThreadingHTTPServer(("127.0.0.1", 0), Model)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="birdhackbot-terminal-") as temporary:
            base = Path(temporary)
            endpoint = f"http://127.0.0.1:{server.server_port}/v1"
            for mode in ["success", "reuse", "auto", "deny", "stop", "cancel", "provider-error", "question", "recovery", "orchestration"]:
                root = base / ("success" if mode == "reuse" else mode)
                root.mkdir(exist_ok=True)
                (root / "AGENTS.md").write_text("Authorized synthetic fixture commands only.\n")
                (root / "go.mod").write_text("module terminal-fixture\n")
                run_case(binary, root, endpoint, mode)
                print(f"guided terminal: {mode} passed", flush=True)
            tui_root = base / "tui"
            tui_root.mkdir(exist_ok=True)
            (tui_root / "AGENTS.md").write_text("Authorized synthetic fixture commands only.\n")
            (tui_root / "go.mod").write_text("module terminal-fixture\n")
            run_tui_smoke(binary, tui_root, endpoint)
            print("guided TUI: smoke passed", flush=True)
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
