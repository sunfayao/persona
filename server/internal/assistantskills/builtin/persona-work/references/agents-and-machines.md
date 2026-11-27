# Agents and computers

For “Which computer is free?” use `machines_list`. Report observed availability and the age of resource samples. If a machine is offline or a measurement is missing, say what is known. Confirm the project directory independently of its remote repository URL.

For “What is my Agent doing?” find the exact machine and session with `agents_list`, then read the relevant turns with `agent_read`. Its default view lists active Agents, then idle ones and those that ended in the last 24 hours, newest first. Each line's `reuse=` says how work goes on in that Agent's own context: plain `agent_send` for a live one, `resume: true` for a stopped one, or only the owner's Resume after the owner's Stop. `state: "all"` lists the active ones and then the rest by latest update. Send a new instruction with `agent_send` only when requested. Interrupt or change the launcher only for the requested Agent. For a bulk switch, preview the selection and follow the tool's confirmation procedure.

If the owner asks to organize sessions, distinguish archiving the list entry from closing its terminal. Use the archive or restore action they requested. Auto Rename for a coding Agent has its own confirmation. When main is asked to bring an existing independent Agent under supervision, use `agent_supervise`.
