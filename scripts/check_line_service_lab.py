#!/usr/bin/env python3
"""Verify the black-box line-service fixture's vulnerable and fixed controls."""

import importlib.util
from pathlib import Path
import socket
import sys
import threading


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "testdata/line-service-lab/server.py"
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("line_service_lab", SOURCE)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def request(port, command):
    with socket.create_connection(("127.0.0.1", port), timeout=2) as connection:
        connection.settimeout(2)
        with connection.makefile("rwb") as stream:
            banner = stream.readline().decode().strip()
            assert banner == "LatticeLedger/0.4 ready", banner
            stream.write((command + "\r\n").encode())
            stream.flush()
            return stream.readline().decode().strip()


def check_mode(mode, private_export):
    with module.Server(("127.0.0.1", 0), module.Handler) as server:
        server.mode = mode
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            port = server.server_address[1]
            assert request(port, "READ public-1") == "OK " + module.PUBLIC
            assert request(port, "EXPORT public-1") == "OK " + module.PUBLIC
            assert request(port, "READ billing-2") == "DENIED private record"
            assert request(port, "EXPORT billing-2") == private_export
            assert request(port, "READ missing-1") == "NOT_FOUND"
            assert request(port, "EXPORT missing-1") == "NOT_FOUND"
        finally:
            server.shutdown()
            thread.join(timeout=2)


if __name__ == "__main__":
    check_mode("vulnerable", "OK " + module.PRIVATE)
    check_mode("fixed", "DENIED private record")
    print("line-service vulnerable and fixed controls: ok")
