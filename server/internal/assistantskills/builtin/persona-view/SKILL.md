---
name: persona-view
description: Write a Persona card (a persona-view code block in a reply, or a live card with view_show / view_update). Read before producing any card - a choice, a comparison table, key numbers, a chart, a progress card, a form - and when a card you wrote was rejected by validation.
---

# Persona cards (persona-view)

A card is a View: the same schema the extensions' UI uses. Write one inline as a fenced code block tagged `persona-view`, or
create a live one with `view_show`. Use a card when the user has to compare, choose, fill in conditions or watch progress; when
one sentence says it, write the sentence.

## Rules for every card

- Conclusions and key numbers still go in the reply's text. Every card has a plain-text `fallback` (that exact field name, at
  most 2000 characters); a single-component shorthand needs one too.
- At most 64 KB per block and 3 cards per reply. A card should read at a glance; details go on a named page or in the text.
- Use only supported components. Do not invent a type (timeline, diff / code, image, tabs, pie) that is not declared supported.
- Fix field errors a tool returns, and Stop-validation feedback, in the same turn: keep finished work and do not repeat actions.
  If you cannot produce a valid card, say it in plain text; a format error is never a new request from the user.

## Components

| Need | Component | Fields |
| --- | --- | --- |
| A choice | `row` / `button` with a `reply` action | `title`, optional `subtitle`, `action` |
| A comparison | `table` | `columns` (`title`), `rows` |
| Key numbers | `metric` / `stat_grid` | `metric`: `label`, `value`; `stat_grid`: `stats`, each `label` and `value`. A value is finished text |
| A trend | `chart` | `kind` `line` / `bar` (stacking: `bar` with `stacked=true`); values are finite numbers; at most 4 `series`, each with a `name`; keep the detail table and do not rely on color alone |
| A process | `progress` | `label`, `ratio` (a number from 0 to 1, such as 0.5), explanation in `detail`; there is no `value` field |
| Conditions | `field` | `name`, `label`, `kind` `number` / `choice` / `toggle` …; `value` and submitted values are strings |
| Text | `markdown` | explanation |
| States | `empty` / `loading` | empty and waiting states |

## Actions

- An action `id` is unique across the card and its pages. Each action is exactly one of `reply`, `open`, `push.page`, `pop`.
- `reply` sends text back to this conversation. `submit=true` goes with `reply` and submits the fields declared on the current
  page; it cannot collect a password, token or secret.
- A card submits once, and an update does not clear the result already chosen: for another round of choices make a new card.
- `confirm` is a native second confirmation the user sees. A card never replaces `ask_user` or a tool approval: a decision you
  must have before going on uses `ask_user`; after sending optional suggestions on a card, end the turn and continue when the
  reply comes, without guessing the choice.
- `open` takes only https links or existing Persona conversation / task / file links, exactly as the system gave them; never
  build a personal address yourself. Links open inside Persona.
- `push.page` points to a page in the root View's `pages`: at most 8 named pages and 4 levels of navigation; pages do not nest
  `pages`; `pop` goes back. A field `name` and a component `key` are unique within their page; keep keys stable across updates.

## Examples

A choice, with details on a page of the same View:

```persona-view
{"title":"Choose a plan","fallback":"Plan A is cheaper, plan B is faster. Choose one to continue.","sections":[{"items":[{"type":"row","title":"Plan A","subtitle":"Cheaper","action":{"id":"choose-a","reply":"Choose plan A"}},{"type":"row","title":"Plan B","subtitle":"Faster","action":{"id":"choose-b","reply":"Choose plan B"}},{"type":"button","title":"Compare","action":{"id":"details","push":{"page":"details"}}}]}],"pages":{"details":{"sections":[{"items":[{"type":"table","columns":[{"title":"Plan"},{"title":"Trait"}],"rows":[["A","Cheaper"],["B","Faster"]]},{"type":"button","title":"Back","action":{"id":"back","pop":true}}]}]}}}
```

Collecting conditions (`submit` takes the current page's fields):

```persona-view
{"title":"Conditions","fallback":"Fill in the budget and the priority, then confirm.","sections":[{"items":[{"type":"field","name":"budget","label":"Budget","kind":"number","value":"100"},{"type":"field","name":"focus","label":"Priority","kind":"choice","value":"speed","choices":[{"value":"speed","title":"Speed"},{"value":"cost","title":"Cost"}]},{"type":"button","title":"Confirm","action":{"id":"submit","reply":"Continue with these conditions","submit":true,"confirm":"Use these conditions?"}}]}]}
```

## Live cards

- For progress or a dashboard that keeps changing, call `view_show` and remember the returned id; afterwards `view_update`
  replaces the same card (update the `fallback` too). It adds no message and does not reopen an answered card.
- `view_get` gives the current version and actions; on an update conflict read first, then change. You read and write only
  this conversation's cards; to alert the user use `notify_user`.
- A progress card as `view_show`'s `view`; on update change `ratio`, `detail` and `fallback`, and keep the `key`:

```json
{"title":"Writing the summary","fallback":"In progress: step 2 of 3.","sections":[{"items":[{"type":"progress","key":"progress","label":"Progress","ratio":0.67,"detail":"Step 2 of 3: grouping the issues"}]}]}
```

## Free-form HTML

Use the `html` component only when the platform declares it supported; otherwise use the components above and the fallback,
and do not assume a bridge exists. Where supported, a page runs when it scrolls into view and is destroyed when it leaves. It
must be self-contained and offline, and cannot read Persona's token, cookies or files. `persona.reply` / `persona.open` each
need a native confirmation; `reply` still submits once and collects no password. Do not describe an ordinary iframe or a ready
timeout as full network or execution isolation.

A page written this way runs everywhere: draw with elements, classes, inline and `<style>` styles, SVG, events and 2D canvas
(shapes, text, gradients); leave layout to CSS (flex / grid / percentages); scripts change content and styles only and never
read element sizes back (`getBoundingClientRect`, `offsetWidth` and the like); images are `<img>` with inline `data:` sources,
and canvas draws no images (no `drawImage`); links go through `persona.open`.
