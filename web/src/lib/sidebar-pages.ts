// The sidebar's pages as it draws them (docs/web/web.md "Sidebar"): the built-in pages in their order, the
// pages extensions' nav_pages add right before Settings, and which of them wait under More.
//
// Groups: the built-in Pages group ("") and the ones extensions' nav_pages name (`group` of the place), first named first;
// Settings stays in Pages. The built-in pages always stay in the list; an extension's page does too while fewer than
// EXTENSION_CAP extension pages are in it, and goes under More after that, so installing a tenth extension does not make
// the list longer.

/** How many extension pages the list takes before the next ones wait under More. */
export const EXTENSION_CAP = 3;

const isExtensionPage = (k: string) => k.startsWith("p:");

/** One group of the sidebar's pages as drawn: its id, its title (none: Pages, which the app names) and its pages. */
export type PageSection<T> = { id: string; title?: string; pages: T[] };

/** The pages split into their groups, in order, and what waits under More (drawn at the end of Pages). */
export function arrangeSections<T extends { k: string; group?: { id: string; title: string } }>(pages: readonly T[]): { sections: PageSection<T>[]; more: T[] } {
  let extensions = 0;
  const shown: T[] = [];
  const more: T[] = [];
  for (const p of pages) {
    if (!isExtensionPage(p.k) || extensions++ < EXTENSION_CAP) shown.push(p);
    else more.push(p);
  }
  const sections: PageSection<T>[] = [{ id: "", pages: [] }];
  for (const p of shown) {
    const id = p.k === "settings" || !p.group ? "" : p.group.id;
    let section = sections.find((s) => s.id === id);
    if (!section) sections.push((section = { id, title: p.group!.title, pages: [] }));
    section.pages.push(p);
  }
  return { sections, more };
}
