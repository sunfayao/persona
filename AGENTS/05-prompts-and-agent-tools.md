# Prompts and agent tools

> How text for models is written, rendered, tested and recognised, and how a tool the assistant can call is defined.
> Read this before you touch `server/internal/prompts`, add or change an MCP tool, or change any text Persona sends
> to a model or types into a conversation. You usually arrive from core constraint 6 of `AGENTS.md`.

---

## Where model-facing text lives

Every prompt, injected notice and tool description is a template under `server/internal/prompts/`. Go code outside
that package never builds prompt text from string literals: it calls a typed render function.

| Piece | Location | Rule |
| --- | --- | --- |
| Template | `prompts/<scene>/<name>.md` | One prompt per file, Go `text/template`, `missingkey=error`. The file's last newline is not part of the prompt |
| Render function | `prompts/<scene>.go`, or a new `prompts/assistant_<topic>.go` | Typed data struct in, string out: `func RenderX(d X) string { return render("scene/name", d) }` |
| Shared parts | `prompts/shared/` | Included with `{{template "shared/name" .}}` |
| Tool description | `prompts/mcp/<scene>/<tool>.md` | A description paragraph, a blank line, a fenced `json` block with the input schema |

A new top-level scene directory must be added to the `//go:embed` list in `prompts/prompts.go`. A render error
panics: a model never receives half a prompt.

- Callers fill data and call the render function. A default wording lives in the template
  (`{{or .Owner "the user"}}`), not in the caller.
- Every template file ends with a newline and is used by some render function; tests fail otherwise. A literal
  `{{` is written `{{"{{"}}`.

## English only

Templates are written in English and contain no branches on language. The owner's language enters in exactly one
way: where the model must answer or write in it, the template prints its name.

```
Write the title in {{lang .Language}}, at most 30 characters.
```

`lang` (`prompts.AnswerLanguage`) prints `Simplified Chinese`, `English` or `Japanese`. The guard
`scripts/ci/prompt_language_ratchet.py` (run by `scripts/test/check-fast.sh`) fails on any Chinese or Japanese
character in a template; its `DATA` table lists the few files that hold such text as data (old default routines,
identifiers of another program), each with the reason.

Text for a model is never taken from the i18n catalogs, and text for a person is never written into a template (`08`).

## Other people's text inside a prompt

Anything a person or another program wrote (a message, a file name, a title) goes through the template function
`ctx`, which keeps it from closing the `<persona-context>` block it sits in; attribute values go through `attr`. The
caller clips long text (`textfmt.Clip`, `textfmt.OneLine`) before rendering. Treat such text as data in the wording
around it too: say whose words they are.

## Goldens

Rendered prompts are pinned byte for byte in `testdata/prompts/*.golden` of the package that renders them
(`assistant`, `hub`, `node`, `voice`).

| You changed | Then |
| --- | --- |
| A template or its data | `UPDATE_GOLDEN=1 go test ./internal/<package> -run Golden`, and read the diff: it is the review of your prompt change |
| A tool description or schema | `UPDATE_GOLDEN=1 go test ./internal/hub -run TestGoldenMCPTools` (also `extmgr`, `mail` for their tools) |
| Nothing in prompts | The goldens must not change. A golden that moves in a refactor means behaviour moved |

You never edit a golden by hand, and you use placeholder data in them (the privacy scan reads them).

A prompt that makes a judgement (is the task done, should this be delegated, how urgent is this message) has live
cases in `assistant/testdata/judge/`. Before and after you edit one, run its live test against a real model
(`PERSONA_LIVE_AI='{"base_url":"...","api_key_file":"...","model":"..."}' go test -run 'JudgeLive' -v
./internal/assistant`) and compare how many cases pass. The golden shows what you changed; only the live run shows
whether the model still decides well.

## Messages Persona types into a conversation

Some templates produce messages that arrive in a conversation as if typed: task notices, scheduled runs, supervision
digests, relays between conversations, answers to a question asked earlier. The server and the web app must recognise
them later in transcripts, so each has a recogniser in `prompts/injected.go` (and `web/src/lib/injected.ts`), tested by
rendering the template and parsing the result, with shared cases in `spec/fixtures/injected/`.

- If you change the first lines or the marker of such a template, you change its recogniser in the same commit.
- Transcripts written earlier still hold the old shapes, some of them Chinese. The recognisers named `legacy...`
  keep reading those. You do not delete a legacy recogniser to tidy up.

## Agent-to-agent messages

A message from one agent to another is wrapped by `prompts/agents/a2a_message.md`. The note at its head depends on
what the recipient can do (it has the Persona tools, it has only a terminal, or neither): tell a recipient only how
*it* can answer. When you add a way for agents to talk, reuse this envelope and the `a2a_*` tools
(`server/internal/assistant/agents`) instead of adding a parallel channel.

## Defining a tool

1. Write `prompts/mcp/<scene>/<tool>.md`. The description says what the tool does, when to use it and what comes
   back, in plain sentences; parameter descriptions say what a value means, not its type. Write for a model that has
   never seen Persona.
2. Add the name to the tool list of the conversation kind that gets it (`toolNames` in `assistant/tools.go`, or the
   list of a narrower kind). `TestEveryMCPToolIsUsed` fails for a description no Go code names.
3. Handle it in the domain: decode with `strictParams(name, args, &p)` (unknown fields are refused), return
   `(string, error)`. Results are text for a model: English, short, with the ids it needs for the next call; errors
   say what to do differently.
4. Regenerate the tool goldens.

A tool name is a wire name: other versions, stored transcripts and extensions know it. You do not rename a tool; you
add one and keep the old name working, or remove a tool whose feature is gone together with every mention of it.

## Size is a cost

Every always-loaded instruction and tool description is paid for in every turn of every conversation. Put what only
some turns need behind a skill (`server/internal/assistantskills/builtin/`) or a tool the model can call, state a
rule once, and delete a sentence before you add one.
