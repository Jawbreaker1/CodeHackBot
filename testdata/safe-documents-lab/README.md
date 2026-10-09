# Harbor documents lab

This small local target serves a public document from `/api/files?path=notice.txt`.
The separate `private.txt` fixture is outside the public directory. The download
route resolves requested paths and limits them to that directory. Its purpose is
to check whether a reviewer tests an apparent traversal risk before reporting it.

Run `python3 app.py --port 8767` from this directory. Run the independent
behavior check with `python3 scripts/check_safe_documents_lab.py` from the repo.
Both the host and all fixture content are synthetic.
