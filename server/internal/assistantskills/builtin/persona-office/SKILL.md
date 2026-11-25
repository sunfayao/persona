---
name: persona-office
description: Create Word (.docx), PowerPoint (.pptx) and Excel (.xlsx) files, or look at what an Office file contains. Use when the user wants a report, document, minutes, proposal, presentation, slides, a spreadsheet, or says "export to Word / PPT / Excel". Files are generated on this machine with personad; Office, Python and Node are not needed. Not for PDF, and not for editing an existing Office file (generate a new one).
---

# Office files (persona-office)

Write the content as Markdown (documents, presentations) or JSON / CSV (spreadsheets), then turn it into an Office file with
`scripts/office.sh` in this skill's directory. It calls `personad office` on this machine; details are in the Persona
repository's `docs/assistant/office.md`.

## Steps

1. Write the content to a file in the current working directory (for example `weekly-report.md`), with any images beside it.
2. Generate: **one command generates one file**, written as
   `sh <absolute path of this skill's directory>/scripts/office.sh docx weekly-report.md -o weekly-report.docx --title "Weekly report"`.
   pptx and xlsx work the same way: `… pptx launch.md`, `… xlsx sales.json`. Without `-o` the file goes beside the source.
   Persona already allows a command written this way, with no approval from the user. Do not build the path from shell
   variables, and do not join several commands with `;`, `&&` or a line break: that raises an approval card for the user.
   `-o` must be a file of the same kind (`.docx` / `.pptx` / `.xlsx`).
3. Read `check` in the command's output: are the title, page count, each page's title, the tables and the number of formulas
   what you wanted? If not, change the source and generate again.
4. In your reply, **put the absolute path of the generated file (`file` in the output) on a line of its own**. Persona shows it
   as a file that opens in a preview, so do not paste the content again.

To see what an existing Office file contains, run `sh <this skill's directory>/scripts/office.sh check <file>`: it lists the
title, the sheets, each page's title and so on.

## Word (docx): write Markdown

- Supported:
  - `#` / `##` / `###` headings and paragraphs; **bold**, *italic*, `code`, ~~strikethrough~~, [links](https://…).
  - Ordered and unordered lists, nested too; an ordered list starts from the first number you write.
  - GitHub tables: the first row is the header, `:---:` sets alignment.
  - Code blocks, `>` quotes, `- [ ]` / `- [x]` checkboxes.
- `---` is a page break.
- Images: `![caption](image.png)`, a path relative to the Markdown file; PNG / JPEG / GIF only, at most the page's width.
  Images on the web are not downloaded; only the caption is kept.
- The page is A4. Body text, headings, tables, code and quotes have one consistent style; do not set font sizes or colors
  yourself (Markdown cannot).

## PowerPoint (pptx): write Markdown, 16:9

- Pages: with `---`, pages break at `---`; without it every `#` / `##` starts a page. A page's first heading is its title.
- Cover: a first page with only a `#` heading and one or two paragraphs becomes the cover, the text its subtitle.
- Use a list for a page's points; numbered lists keep their numbers. Other paragraphs are lines without bullets. A page takes at
  most one image and one table (more are left out): with text the image goes on the right and the table below; with both, the
  image is on the right and the text and table stack on the left.
- Keep a page light: about 3 to 6 points of one line each. The font shrinks with the amount of text (24 to 14 pt); when it is
  crowded, split the page in two.
- There are no speaker notes.

## Excel (xlsx): write JSON or CSV

```json
{"sheets":[{"name":"Sales","header":["Month","Revenue","Cost","Profit","Date"],
  "rows":[["Jan",1200.5,800,"=B2-C2","2026-01-31"],
          ["Feb",1350,900,"=B3-C3","2026-02-28"],
          ["Total","=SUM(B2:B3)","=SUM(C2:C3)","=SUM(D2:D3)",null]],
  "formats":["","#,##0.00","#,##0.00","#,##0.00","yyyy-mm-dd"],
  "widths":[8,12,12,12,12]}]}
```

- A cell is text, a number, `true` / `false`, `null` (empty), or a formula starting with `=`. Rows count from 1 and the header is row 1.
- **Write totals, differences and shares as formulas instead of computed numbers**: when the user changes the data, the results follow.
- Formula values are computed when the file is generated and written into it, so a preview (Persona's too) shows them. These are computed:
  - `+ - * / ^ & %` and comparisons;
  - cells, ranges and other sheets: `Sales!B2`; a name with a space needs quotes, `'Sales 2026'!B2`;
  - SUM AVERAGE MIN MAX COUNT COUNTA ROUND ABS IF IFERROR AND OR NOT.
  - Other functions (VLOOKUP and so on) can be written too, but are computed only when the file is opened in Excel / Numbers; a preview shows them empty.
- `formats` are Excel number formats per column (`0.00`, `#,##0`, `0%`, `yyyy-mm-dd`). In a date-formatted column write `"2026-01-31"`; it becomes a real date.
- The header is bold and frozen by default (`"freeze": false` turns that off), and column widths follow the content.
- CSV: the first row is the header; what looks like a number is a number, and an id such as `007` stays text. In a CSV, anything
  starting with `=` (in the header too) stays text and is not a formula: write JSON when you need formulas.

## Not supported

- Editing an existing docx / pptx / xlsx: generate a new one.
- PDF, charts, speaker notes, custom fonts and themes, merged cells, multi-level headers.
- For a chart, draw it as a PNG some other way first, then put it in the document or slide.
