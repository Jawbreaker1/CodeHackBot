"""Small line-oriented assessment target with vulnerable and fixed modes."""

import argparse
import socketserver


PUBLIC = "The maintenance window is Saturday."
PRIVATE = "SYNTHETIC-PRIVATE-RECORD: internal billing note for fixture tenant B."


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        self.wfile.write(b"LatticeLedger/0.4 ready\n")
        while line := self.rfile.readline(4096):
            parts = line.decode("utf-8", "replace").strip().split()
            if not parts:
                self.reply("ERR empty command")
                continue
            command = parts[0].upper()
            if command == "QUIT":
                self.reply("BYE")
                return
            if command == "HELP":
                self.reply("COMMANDS HELP VERSION STATUS INDEX READ <id> EXPORT <id> QUIT")
            elif command == "VERSION":
                self.reply("LatticeLedger 0.4; assessment fixture build")
            elif command == "STATUS":
                self.reply("records=2; private records require authorization")
            elif command == "INDEX":
                self.reply("public-1 public; billing-2 private")
            elif command in {"READ", "EXPORT"} and len(parts) == 2:
                self.record(command, parts[1])
            else:
                self.reply("ERR unknown command")

    def record(self, command, record_id):
        if record_id == "public-1":
            self.reply("OK " + PUBLIC)
        elif record_id == "billing-2":
            if command == "EXPORT" and self.server.mode == "vulnerable":
                self.reply("OK " + PRIVATE)
            else:
                self.reply("DENIED private record")
        else:
            self.reply("NOT_FOUND")

    def reply(self, value):
        self.wfile.write((value + "\n").encode("utf-8"))


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--mode", choices=("vulnerable", "fixed"), required=True)
    args = parser.parse_args()
    with Server(("127.0.0.1", args.port), Handler) as server:
        server.mode = args.mode
        print(f"LatticeLedger listening on 127.0.0.1:{args.port}", flush=True)
        server.serve_forever()


if __name__ == "__main__":
    main()
