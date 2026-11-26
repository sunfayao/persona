# To-do layout fixtures

`todo_layout.json` has five cases for the browser's To-do layout. Each case names the window, its
dimensions, text scale, requested regions and previous layout, and lists the resulting forms and widths.

The sidebar is 284 points wide and its rail is 56. At the default text scale, the list is 360, the paper
has a 400-point minimum, and the conversation is 440. The paper takes the remaining space. A previous
rail can stay visible while the window is short by up to 24 points, and a full sidebar returns when it fits.

| Case | Window | Wants | Regions shown |
| --- | --- | --- | --- |
| `web_1440_closed` | 1440 × 900 | paper | Sidebar 284 │ List 360 │ Paper 796 |
| `web_1440_open` | 1440 × 900 | conversation | Rail 56 │ List 360 │ Paper 584 │ Conversation 440 |
| `web_1600_closed` | 1600 × 1000 | paper | Sidebar 284 │ List 360 │ Paper 956 |
| `web_1600_open` | 1600 × 1000 | conversation | Sidebar 284 │ List 360 │ Paper 516 │ Conversation 440 |
| `web_band_holds_the_rail_not_the_sidebar` | 1460 × 900, after the rail | conversation | Rail 56 │ List 360 │ Paper 604 │ Conversation 440 |
