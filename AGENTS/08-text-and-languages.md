# Text and languages

> Which text is translated, which is English, and how each is added. Read this before you write any string a person
> or a model will read. You usually arrive from core constraint 7 of `AGENTS.md`.

---

## Three kinds of text

| Reader | Language | Where it lives | How it is produced |
| --- | --- | --- | --- |
| A person using the product | Simplified Chinese, English and Japanese | Catalogs: `server/internal/i18n/locales/<lang>/<namespace>.json`, `web/src/i18n/locales/<lang>/<namespace>.json` | `i18n.T` / `i18n.Err` on the server, `t()` in the web app |
| A model | English | `server/internal/prompts/` (`05`) | A render function. Never an i18n key |
| A developer or a program | English | In the code | Logs, comments, documents, commit messages, validation errors of an API, JSON output of commands |

The mistake to avoid is mixing them: a catalog key inside a prompt, a Chinese literal in Go, a prompt sentence in a
catalog, an English-only error shown to the owner.

## Adding text for a person

**Server**

1. Add the key to the namespace file in all three languages, with the same placeholders:
   `locales/en/assistant-widgets.json` → `"limit_reached": "You can keep at most {max} widgets."`
2. Use it: `i18n.T(lang, "assistant-widgets.limit_reached", i18n.Args{"max": n})`, or return
   `i18n.Err("assistant-widgets.limit_reached", i18n.Args{...})` and let the edge localize it.
3. The language comes from the request or from the owner's locale (`s.lang(ctx)`), never from the machine.

**Web**

1. Add the key to `web/src/i18n/locales/<lang>/<namespace>.json` in all three languages. One namespace per page or
   feature; `common` only for words that really are shared; `admin-*` only for the admin pages.
2. Use it: `t("widgets.limit_reached", { max })`. Plurals use `_one` / `_other` keys with a `count`. A sentence with
   a link or emphasis inside uses `tRich`. Dates, numbers and lists use the formatters of `src/i18n`.
3. A page that is not on the first screen keeps its strings out of the namespaces listed in
   `src/i18n/startup-namespaces.json` (`07`).

## What the tests require

| Rule | Server (`go test ./internal/i18n`) | Web (`src/i18n/catalog.test.ts`) |
| --- | --- | --- |
| The same namespaces, keys and placeholders in every language | `TestCatalogsAgree` | yes |
| Every literal key exists and gets its placeholders | `TestCallSitesUseKnownKeys` | yes |
| Every key is used; an unused key is deleted from all languages | `TestEveryKeyIsUsed` | yes |
| No new Chinese literal in code that people see | `TestHardcodedChineseBaseline`, a per-file count that may only fall | the same, `hardcoded-baseline.json` |

So removing the last use of a key means removing the key, in the same change, from every language. A key built at
run time is found only through a literal prefix: keep the prefix spelled out.

A Chinese literal that must stay in code (text matched against another program's output, a value stored earlier, a
legacy shape a recogniser reads) carries a comment on its line: `// i18n-ignore: <why>`.

## Writing the words

- Write whole sentences in each language; do not glue translated halves together.
- "Persona" is not translated. Product words follow the glossary in `docs/protocol/i18n.md`.
- Say what happened and what the person can do next, in plain words, without internal names (no RPC names, table
  names, ids or paths in primary text).
- The three translations say the same thing. If you cannot write one of them well, write it plainly rather than
  leaving it out: a missing key falls back to English, then Chinese, then the key itself, and the tests fail first.

## Comments, documents and commits

English, full sentences. A comment says what the code does and why it has to; it does not tell the story of how the
code got there, and it does not cite issue numbers of the upstream project or the removed native apps (old comments
do: leave them or delete them, do not imitate them).
