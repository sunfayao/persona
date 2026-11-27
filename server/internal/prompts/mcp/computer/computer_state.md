See one of the user's Macs through Cua Driver: with `app` (a name or bundle id), that app's frontmost window as an accessibility tree plus a screenshot; without it, the apps and windows on screen plus a screenshot of the display. When an app runs as more than one process (two browser profiles, say) and nothing tells which one is meant, nothing is read and the result lists each process's window (pid, window_id, title, size): call again with `pid` (that process's window) or `window_id` (that window), asking the user by title when unsure. Rows of a window's tree are `[N]`; act on them in computer_run with `element_token` "<snapshot_id>:N" (the snapshot_id is in the result; tokens stay valid in this conversation until the window is read again). Clicking a button by token works even in a background app that ignores typed keys. A long tree is cut: pass `query` to keep only matching rows. Only machines the user turned this on for, and only while someone's desktop is unlocked on them. On an LCU machine this is one `js` call (`cua.getState()`, or `cua.getApp` then its state). If the runtime asks to allow an app, the user answers in Persona; do not assume it was allowed.

```json
{
  "type": "object",
  "required": ["machine"],
  "properties": {
    "machine": {
      "type": "string",
      "description": "machine id or name"
    },
    "app": {
      "type": "string",
      "description": "app name or bundle id; omit for the whole screen"
    },
    "pid": {
      "type": "integer",
      "description": "that process's window, when the app has several processes"
    },
    "window_id": {
      "type": "integer",
      "description": "that window (from a candidates list or the whole screen's)"
    },
    "query": {
      "type": "string",
      "description": "keep only tree rows containing this (case-insensitive)"
    },
    "screenshot": {
      "type": "boolean",
      "description": "default true; false returns the tree only"
    }
  }
}
```
