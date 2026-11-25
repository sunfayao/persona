# permission-modes

The standalone name the web gives each permission mode (#10363), using the wording settled in #5378. Keys are
the mode IDs `default`, `acceptEdits`, `plan`, `auto` and `bypassPermissions`; every entry has exactly `en`, `zh-Hans`
and `ja`. These are names, not descriptions, approval-button actions, launcher-default choices, raw-ID handoff formats
or unset/default-behavior labels.

The web keeps its normal localization resources; it does not load this JSON at runtime. A check reads
this table and its resources so the names cannot drift:

- Web: `web/src/i18n/permission-mode-names.test.ts` checks the picker, answered-plan and planned-task labels, plus
  Full access confirmations.

Add a mode here first, then register every standalone localized alias in the web check. Update the table and the web resources together.
