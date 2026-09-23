# Codex archive recovery baseline — 2026-09-23

Scope: an operator-owned local `secret.zip` in the isolated Kali workspace. The original
SHA-256 was `af82d38ac307097ed052739d20487fbbe9231f725993983f62a866ddb49225dd`
before and after the experiment. No network target was contacted. The password
and extracted text are deliberately absent from this document.

## Observed result

`zipinfo -v` identified one 243-byte, deflated, traditionally encrypted entry,
`treasure-note.txt`. A fresh `zip2john` conversion and isolated John pot files
were used for the independent attempts below:

| Attempt | Bounded coverage | Result |
| --- | --- | --- |
| John `--single` | Archive/filename-derived candidates | No recovery |
| Context-derived local dictionary | 12,060 candidates plus `l33t`, year/special, and number/special John rules | No recovery |
| John `--stdin` with packaged RockYou | All 14,344,392 source records; roughly one second of John runtime | No recovery |
| `fcrackzip -b -c aA1 -l 1-4 -u` | One-to-four-character letters and digits; 48 seconds | No recovery |
| John `--rules=AppendJustNumbers` with the first 500,000 RockYou entries | Standard numeric-suffix mutations; fresh isolated pot | **Recovered in 4.1 seconds** |
| John `--rules=AppendJustNumbers` with the complete RockYou list | Same rule against all packaged entries; another fresh isolated pot | **Recovered in 100.0 seconds** |

The successful command used John the Ripper's packaged `AppendJustNumbers`
rule, without a custom wordlist or known password. The 500,000-entry fast pass
is a useful staged workflow, but it was selected **after** inspecting the
recovered credential's shape. It demonstrates that ordinary Kali tooling can
recover this archive quickly; it does not prove an agent would have selected
that strategy unaided. The complete-list run avoids the prefix choice and
independently recovered the credential in under two minutes. The earlier
conclusion that a fresh search had failed was incomplete because it omitted
this common mutation rule. The fresh complete-list pot independently decrypted
the current archive with Python's ZIP reader and produced the same 243-byte
entry hash recorded below.

An archived February 2026 assessment already contained a valid candidate in
`sessions/run-20260223-172436-zipsecret5/orchestrator/artifact/t2/john_secret.pot`.
Its saved task explicitly ran `john --show` against John's **default pot before**
trying a dictionary, and its status recorded `source=default_pot`. That earlier
run is evidence of credential reuse, not evidence that the run independently
cracked the archive. This does not establish how the credential originally
entered the default pot or whether other older runs cracked it independently.
The old pot hash does not match the current archive hash
(`john --show` against today's conversion reports zero cracked), but the
credential itself still decrypts the current archive. It is absent from the
packaged `fasttrack.txt`, `password.lst`, and unmodified RockYou lists; its
base word does occur in RockYou.

The valid credential was checked against the current archive with Python's ZIP
reader, then used to extract into
`/tmp/birdhackbot-codex-zip-baseline/extracted/`. The extraction code rejected
absolute/traversal paths and links, limited member size, read through the ZIP
reader's CRC check, and wrote a new file with mode `0600`. The resulting
`treasure-note.txt` is 243 bytes with SHA-256
`f0ce3764500c295d9480ec47015a98a09202170bf905cdaf5555b32319d0347a`.
The local manifest is `/tmp/birdhackbot-codex-zip-baseline/baseline-result.json`.

## Framework comparison point

The useful general behavior is to distinguish **recovery of an existing,
provenanced credential** from a fresh candidate search, and to cover common
mutations before declaring a wordlist miss. A worker should inspect the archive
and available tools, check relevant authorized prior assessment evidence, run a
bounded staged search with ordinary Kali rules where appropriate, independently
validate any candidate against the current artifact, then extract safely. If
no candidate exists, it should report exact coverage and plan the next useful
strategy. The model and harness should choose and track this sequence without
being prompted with a specific wordlist or rule.
