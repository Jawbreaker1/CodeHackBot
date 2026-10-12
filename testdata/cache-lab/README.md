# AccountDesk test service

This local fixture uses only fabricated accounts and records. Start it with
`python3 testdata/cache-lab/app.py --ready-file /tmp/accountdesk-url` and read
the assigned loopback URL from that file. Stop the process after testing.

The operator can give an assessment worker the URL, the two test credentials
in `accounts.py`, and permission for bounded HTTP requests and source review
inside this directory. The worker should report only behavior supported by
its own target-side evidence. The separate oracle is kept outside the worker's
assessment inputs.
