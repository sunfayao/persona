# Port forwarding: viewing pages an Agent serves

An Agent starts a dev server on one of your computers (vite, next dev, `python -m http.server`…). The hub can forward a TCP connection to that port over one WebSocket per connection (`…/ports/{port}/connect`),
so HTTP, hot reload and other protocols pass through untouched. The web client shows such a page through its Preview panel instead.

## At a glance

```
Client                                hub                                node (that computer)
local listener 127.0.0.1:5173
                                      │ one WebSocket per TCP connection
                                      ▼
                         GET /api/v1/machines/{mid}/ports/5173/connect (device token)
                                      │ opens a stream (stream.*) on the existing hub↔node connection
                                      ▼
                                                       connects to local 127.0.0.1:5173 (or the address it was bound to when scanned)
```

## Discovering ports (node, `server/internal/node/ports/ports.go`)

- **Who owns it**: for each live Agent's pane, take the process the host reports (herdr's foreground process; the first one is claude / codex itself),
  walk all its descendants, and add the processes on the same terminal (tty) (including ones adopted by init after `npm run dev &` and the outer shell exiting).
  Listeners belonging to claude / codex themselves (IDE bridge, MCP) do not count. herdr lists the foreground group's leader first, which is a wrapper
  when the launcher runs claude under one (`<wrapper> run -- <claude arguments>`), so the herdr client puts Claude Code's own process first: the one named
  after a version (Claude Code names its process so), else one whose name or program file says claude. herdr 0.9.5 reports a process's `argv`, not
  `argv0`. Taking the wrapper for the agent listed Claude Code's own loopback listener as a server it runs, which kept an idle Agent from Close (#14681).
  The host is asked for a pane's foreground processes only when the pane is new or its first process has changed (#5515): while that process is still in the
  process table with the same parent, terminal and name, the last answer is used again, less any other process in it that exited or whose pid now names a different process (a reused pid could belong to another pane). A shell first (the Agent not started yet) is asked for on every scan,
  so claude starting under it is not taken for one of the pane's servers. Before this, every scan made one herdr call per pane (about 10 calls/s with 40 Agents at work).
- **How it scans**: on Linux it reads `/proc` directly (`/proc/*/stat` for the process tree; LISTEN sockets in `/proc/net/tcp{,6}` are matched to process fds by inode),
  with no child processes. On macOS it uses `ps -Ao pid=,ppid=,tty=,comm=` plus `lsof -a -p <these processes> -nP -iTCP -sTCP:LISTEN -Fpcn`.
  `lsof` exits 1 when one of the pids has exited / is a zombie but still prints results, so results are used as-is; when a scan really fails, the same error is logged only once.
  On macOS one scan costs about 0.17 s CPU (on a machine with about 3,700 processes, `ps` is most of it).
  Windows (#2098) also spawns no child processes: the process tree comes from a Toolhelp snapshot (`CreateToolhelp32Snapshot`), listening sockets and their owning processes come from iphlpapi's
  `GetExtendedTcpTable` (`TCP_TABLE_OWNER_PID_LISTENER`, one table each for IPv4 and IPv6), in `server/internal/node/ports/ports_windows.go`.
  Windows has no controlling terminal, so ownership is by process tree only. After a parent exits, Windows still reports its pid, and pids get reused, so when the parent started later than the child
  (or its start time can't be read) the parent-child link is ignored, so a new process doesn't "adopt" someone else's children.
  Git Bash breaks that tree (#9815): MSYS2 emulates `fork`, and for `cd x && cmd` the forked copy that runs `cmd` as a new Windows process exits, so `cmd`'s parent pid names a process that is gone. The scan reads every listener anyway, so a listener no pane's tree owns has its process's engine pane variables read (`ORCA_PANE_KEY`, `HERDR_PANE_ID`…, which MSYS passes on; cached per process) and counts for the pane they name when that pane is being scanned. The pane's agent itself stays out, as from the tree (`paneListeners`). On a machine with about 650 processes and 65 listening sockets a scan took 15 ms without that and 18 ms with it.
- **What counts**: for each `pid:port`, send one `GET /` (`Host: localhost:port`, which dev servers' host checks accept). Only ones that answer with HTTP are listed,
  and the home page's `<title>` (up to 60 characters) is captured along the way. One that accepts the connection but says nothing for 3 seconds (next dev compiling for the first time) is listed without a title and asked again on the next scan, up to 5 times.
  Ones that don't answer with HTTP (ssh, databases) are not listed. Results are cached per `pid:port` and dropped when the process or port goes away.
- **How often**: every 15 seconds while there is a live Agent. When an Agent's transcript changes (usually a tool ran; starting / stopping a server is one tool call)
  or a forward finds the port gone, an extra scan runs within 4 seconds. No Agents, no scanning.
- **How it is pushed**: results go in `Session.ports` and travel with the session list. If the port set (port, pid, title) hasn't changed, nothing is pushed (`listKey` includes it),
  so scanning adds no extra pushes (the lesson of #178: don't push what hasn't changed, don't make the UI recompute).

## Recognizing links in replies

- `http(s)://localhost|127.0.0.1|0.0.0.0|[::1]|*.localhost|this machine's hostname (with or without .local):port/path` all count as "a port + path on this machine",
  as does the scheme-less `localhost:3000/x` that servers often print. Links without a port, LAN addresses and other hosts don't count (LAN addresses open as ordinary web pages).
- "This machine" is the machine the session runs on (for assistant chats, the hub machine).
- A link to one of the hub's own pages (`/s/…`, `/todos`, `/persona/…`; a hub on the agent's machine) is no dev server: it gets no badge and opens as that page (#2696).
- The web (#504) recognizes the same links (`web/src/lib/port-links.ts`) and draws the badges under an agent's finished answer (`views/chat/PortChips.tsx`): `:5173` with the page's
  title, "Stopped" and dimmed once nothing listens there. A port some agent on that machine lists (`Session.ports`) is live; any other is asked about with `GET …/ports/{port}`
  (`lib/port-presence.ts`: a live answer is kept a minute, a gone one 30 s, no answer 20 s). A badge, or the link itself, opens the page in the **Preview** panel ([preview.md](../web/preview.md)),
  only on a hub with `preview` and for an agent's session with a working folder; assistant conversations get none.
## Forwarding

### hub (`server/internal/hub/ports.go`) and streams (`server/internal/streams`)

- `…/connect` checks first, then upgrades: the port must be in the `ports` of some session on that machine, or the user must have `POST …/open`ed it within 24 hours; otherwise 403.
  Then it registers a stream and asks the node to `port.dial`. If that fails it returns 404 (not yet upgraded, so the client gets the status code); only on success does it upgrade to WebSocket.
- A stream uses `stream.data`, `stream.ack` and `stream.close` events on the hub↔node JSON-RPC connection (`docs/protocol/protocol.md`). Each direction allows at most 512 KB
  unacknowledged; at that point the sender waits. A large file from a page can't fill the connection and starve other RPCs, and a slow reader can't make the other end buffer without limit.
  Event handling (`Mux.Event`) never blocks, because it runs in the RPC read loop.
- TCP endpoints that negotiate stream version 1 can send `stream.eof` to half-close one direction. A client's binary WebSocket path uses full close.
- At most 64 streams per machine at once (a page typically uses about 6); when a machine disconnects all its streams are closed.
- The slow-request log skips `…/connect` (it is long by nature).

## Security boundary

- Auth is the same as other APIs: device token (or pairing token). Read-only devices always get 403, even for an Agent's port.
- Only ports on **the configured machine itself** can be reached: the node only dials the address that listener was bound to when scanned (a local one) or 127.0.0.1 / ::1, and accepts no hostnames. It is not a proxy that can open arbitrary intranet addresses.
- The port must belong to an Agent (found by the node's scan) or have been opened by the user (`POST …/open`); guessing a port number gets 403. Tapped-open ports are kept in hub memory for 24 hours
  and cleared on hub restart (just open it again).
- Probing (`GET …/ports/{port}`) only reports "is something listening, is it HTTP, title"; it grants no access.
- Exception: when the `cua` plugin is on for a Mac, that Mac's 5900 (macOS Screen Sharing) is permanently allowed, for "VNC" (see [computer-use.md](../extensions/computer-use.md), "Live view"); the WebSocket accepts the `binary` subprotocol (noVNC may send it).
