<!--
Optional linked context:
Add a visible `Closes #<issue-number>` or `Related: #<issue-number>` line
below this comment.

Write for someone who uses or operates Clawgo but has not read the code. The title
and opening problem statement should make sense without the diff or linked issue.

Suggested PR title:
type: <what changes for the person affected>
Use a parenthesized scope only when it helps that reader.

Name the affected task and the symptom or outcome, not the implementation.
These examples illustrate supported Clawgo workflows, not reports of current bugs:
Good: feat: read voice transcripts from a named pipe
Bad: feat(stdin): implement the stdin-file input path
Good: improve: let operators limit how long each spoken reply can run
Bad: improve(tts): add a context deadline to the subprocess

For internal-only work, name the actual developer or operator workflow affected.
-->

## What Problem This Solves

<!--
Describe the concrete user, product, or operational problem.
In 1–3 plain sentences, say who is affected, what happens today, and why it
matters to them. For a bug, name the trigger if known. For a feature or internal
work, describe the need in the actual workflow without inventing harm.
Could a reader understand the problem without knowing your proposed fix?

Good: "Background checks can use up a new user's model allowance before they've
asked the assistant to monitor anything."
Bad: "Turn off periodic checks on fresh installs."
-->

## User Impact

<!--
"User impact: <what users, operators, or developers can now do or expect>."
Lead with the concrete outcome in plain language, usually one sentence.
For internal-only changes, say there is no user-visible change; do not invent a benefit.
Keep important risks, breaking changes, migrations, and required user actions visible here.
-->

## Why This Change Was Made

<!--
Briefly explain how the change addresses the problem without repeating the impact.
Keep the body short. Leave file lists, internal acronyms, and root-cause walkthroughs
in the diff or optional <details>; include technical detail only when it explains
behavior or a material tradeoff. Do not hide risks or required actions in <details>.
-->

## Evidence

<!--
Show the most useful proof that this change works. Terminal output, focused tests,
CI results, live observations, redacted logs, and artifact links are all useful.
For node behavior, say which workflow and environment were checked, such as
pairing, stdin/FIFO transcripts, chat replies, or local TTS on Linux or a Pi.
Distinguish automated checks from a live gateway or device check; say what was not tested.

Start with the behavior checked and the result in plain language. Reviewers will
inspect the code, tests, and CI; do not make them decode command lists to learn
what was verified. Put exact commands, commit hashes, and long output in links
or optional <details>. Keep the useful evidence summary and meaningful gaps visible.
-->
