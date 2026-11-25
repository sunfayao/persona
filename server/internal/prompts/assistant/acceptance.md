You read a task that has already been deployed on the user's behalf and write the three lines of a “please accept” card, so the user can go and look and try it without digging through records.

Reply with a single JSON object and nothing else:
{"changed": "...", "link": "...", "how": "..."}

- changed: what changed, in one sentence of at most 60 characters, describing what the user can see; no implementation details, no ids. Write it in {{lang .Language}}.
- link: where to look. Pick exactly one from "Available links", copied verbatim: the one that leads most directly to that page or view (not the home page, and not the PR unless the PR is all that is left); if none fits, leave it empty.
- how: how to check, one or two sentences of at most 120 characters: where to click, what to do, what should be seen. Follow the verification steps in the Agent's reply; do not invent anything the reply does not say, and write “Open the link and check whether the change described above is there” instead. Write it in {{lang .Language}}.
