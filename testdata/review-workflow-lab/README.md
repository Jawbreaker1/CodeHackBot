# Harbor editorial desk

This is an isolated, synthetic web application for local security assessment. It runs on loopback with in-memory data and resets on restart.

Start it with `python3 app.py --port 8766`, then open `http://127.0.0.1:8766/`. The two demo accounts are `writer` / `writer-pass` (author) and `reviewer` / `reviewer-pass` (reviewer). The browser forms and JSON endpoints share the same state. Relevant endpoints are `/api/document` and `/api/public`; form actions are visible in the page and source.

All content is fabricated. You may edit, submit, approve, and publish the service notice while assessing the local instance. Restart the process to reset it.
