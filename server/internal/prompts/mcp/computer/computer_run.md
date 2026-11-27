Act on one of the user's Macs through Cua Driver in one call: `steps` (actions in order, stopping at the first failure) or `script` (JavaScript). Apps act in the background without taking focus.
- steps: `[{"click": {"app": "Calculator", "role": "button", "name": "7"}}, {"press_key": {"app": "Calculator", "key": "return"}}]`. Only click, double_click, right_click, set_value {value}, type_text {text}, press_key, hotkey {keys}, scroll, drag and move_cursor run as steps: open an app with script `await cua.launch("Calculator")`. A step names its target by role/name or an `element_token` from computer_state. `observe: {}` returns what the steps changed in the window.
- press_key takes key names: return, tab, escape, space, delete, arrows, f1–f12, a letter or digit, with modifiers (cmd+s). Symbols (*, $, …) go through type_text.
- Background apps may ignore typed keys: press a button by clicking it (by element_token from computer_state or observe, or by role/name), and check with observe.
- script: the body of an async function using `cua`: `const app = await cua.getApp("Calculator"); await app.click({role: "button", name: "7"}); const [d] = await app.query({role: "statictext"}); return d.value;`. Also `cua.launch(name)`, `app.typeText(text, target?)`, `app.pressKey("cmd+s")`, `app.waitFor({…})`, `app.getState()`.
- An app running as several processes: pass `pid` (or `window_id`) on computer_run, not `app` in the steps; naming it otherwise is refused with its processes' windows.
Read with computer_state first when unsure what is on screen, or for a screenshot to show the user. Writes may need the user's confirmation in Persona.

Never System Settings or macOS's permission and password prompts: a call that names one or would act in one is refused: the user does that themselves. With System Settings open, steps on other apps (by name or element token) still run, but a point where it is on top, keys with no app while it is in front, and scripts are refused. Nothing runs while a prompt is on screen. A script names every app it touches with a literal `cua.getApp("…")` or `cua.launch("…")`, or is refused. Steps naming no app run one at a time.

On an LCU machine, `script` is LCU's JavaScript and `steps` are translated into it; its confirmations come as Persona cards. A refusal is the user's: do not retry or approve it yourself.

```json
{
  "type": "object",
  "required": ["machine"],
  "properties": {
    "machine": {
      "type": "string",
      "description": "machine id or name"
    },
    "steps": {
      "type": "array",
      "items": {
        "type": "object"
      },
      "description": "Cua run_steps steps"
    },
    "app": {
      "type": "string",
      "description": "default app for steps that name none"
    },
    "pid": {
      "type": "integer",
      "description": "steps' default process, for an app with several"
    },
    "window_id": {
      "type": "integer",
      "description": "steps' default window"
    },
    "observe": {
      "type": "object",
      "description": "what changed after the steps (get_window_state options; {} for defaults)"
    },
    "script": {
      "type": "string",
      "description": "Cua run_script JavaScript"
    },
    "timeout_ms": {
      "type": "integer",
      "description": "script wall time, default 30000, max 120000"
    }
  }
}
```
