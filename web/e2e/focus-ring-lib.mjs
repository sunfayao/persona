// Shared by the focus ring checks (focus-ring-room.mjs, chat-end-clearance.mjs): RING_PAGE, injected with
// addInitScript, gives the page `window.__ring`; ringPixels samples a measured ring's band on its four sides.
// In the page: the ring of the focused control, the boxes that clip it, and who rendered them.
export const RING_PAGE = String.raw`
window.__ring = (() => {
  const px = (v) => parseFloat(v) || 0;
  const name = (el) => {
    const key = Object.keys(el).find((k) => k.startsWith("__reactFiber$"));
    const names = [];
    for (let f = key && el[key]; f && names.length < 3; f = f.return) {
      const t = f.type;
      const n = typeof t === "function" ? t.displayName || t.name : t && typeof t === "object" ? t.displayName || t.render?.name || t.type?.name : "";
      if (n && !names.includes(n) && !/^(Suspense|Provider|Consumer|Fragment)$/.test(n)) names.push(n);
    }
    return names.join(" < ");
  };
  const describe = (el) => {
    const data = [...el.attributes].filter((a) => a.name.startsWith("data-") && a.name !== "data-state").map((a) => a.value ? a.name + "=" + a.value.slice(0, 24) : a.name);
    const label = el.getAttribute("aria-label") || el.getAttribute("title") || (el.textContent || "").trim().replace(/\s+/g, " ");
    return { tag: el.tagName.toLowerCase(), data: data.slice(0, 4), label: label.slice(0, 48), cls: String(el.className?.baseVal ?? el.className ?? "").slice(0, 220), owner: name(el) };
  };
  // the ring's reach past the border box: an outline (width + offset), else the widest ring-like box shadow
  const reach = (el) => {
    const cs = getComputedStyle(el);
    if (cs.outlineStyle !== "none" && px(cs.outlineWidth) > 0) return { kind: "outline", reach: px(cs.outlineWidth) + px(cs.outlineOffset), width: px(cs.outlineWidth), offset: px(cs.outlineOffset), color: cs.outlineColor };
    let best = null;
    for (const part of cs.boxShadow === "none" ? [] : cs.boxShadow.split(/,(?![^(]*\))/)) {
      if (/\binset\b/.test(part)) continue;
      const nums = (part.match(/-?[\d.]+px/g) || []).map(px);
      const [x = 0, y = 0, blur = 0, spread = 0] = nums;
      if (x || y || blur || spread <= 0) continue;
      const color = (part.match(/rgba?\([^)]*\)|#[0-9a-f]+/i) || [""])[0];
      if (/rgba?\([^)]*,\s*0\)$/.test(color)) continue; // a transparent shadow is no ring
      if (!best || spread > best.reach) best = { kind: "shadow", reach: spread, width: spread, offset: 0, color };
    }
    return best;
  };
  const clipBoxes = (el) => {
    const boxes = [];
    for (let a = el.parentElement; a; a = a.parentElement) {
      const cs = getComputedStyle(a);
      const paint = /paint|strict|content/.test(cs.contain) || cs.contentVisibility === "auto";
      const x = cs.overflowX !== "visible", y = cs.overflowY !== "visible";
      if (!x && !y && !paint) continue;
      if (a === document.documentElement || a === document.body) continue;
      const r = a.getBoundingClientRect();
      const margin = (cs.overflowX === "clip" || cs.overflowY === "clip" || paint) ? px(cs.overflowClipMargin) : 0;
      const left = r.left + px(cs.borderLeftWidth), top = r.top + px(cs.borderTopWidth);
      const scrolls = /auto|scroll/.test(cs.overflowX + cs.overflowY);
      // a scroller centres a focused control in what its scroll padding leaves (Chrome): the clear region it can show
      const clearW = a.clientWidth - (scrolls ? px(cs.scrollPaddingLeft) + px(cs.scrollPaddingRight) : 0);
      const clearH = a.clientHeight - (scrolls ? px(cs.scrollPaddingTop) + px(cs.scrollPaddingBottom) : 0);
      boxes.push({ el: a, x: x || paint, y: y || paint, left: left - margin, top: top - margin, right: left + a.clientWidth + margin, bottom: top + a.clientHeight + margin, scrolls, clearW, clearH });
    }
    boxes.push({ el: null, x: true, y: true, left: 0, top: 0, right: innerWidth, bottom: innerHeight, scrolls: false });
    return boxes;
  };
  const visible = (el) => {
    if (el.closest("[inert],[aria-hidden=true],[hidden]")) return false;
    const r = el.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) return false;
    const cs = getComputedStyle(el);
    return cs.visibility === "visible" && cs.display !== "none" && parseFloat(cs.opacity) > 0.05 && !el.disabled;
  };
  return {
    controls(within) {
      const root = within ? document.querySelector(within) : document;
      const all = [...(root?.querySelectorAll("button, a[href], summary, [tabindex]:not([tabindex='-1']), input:not([type=hidden]), textarea, select, [contenteditable=true]") ?? [])];
      return all.filter(visible).map((el, i) => (el.setAttribute("data-ring-audit", String(i)), i));
    },
    measure(i) {
      const el = i === undefined ? document.activeElement : document.querySelector("[data-ring-audit='" + i + "']");
      if (!el || !visible(el) || document.activeElement !== el) return { skip: "not focused" };
      if (!el.matches(":focus-visible")) return { skip: "no :focus-visible" };
      // still see-through while focused (a bar that shows on hover only): no ring to judge, but focus lands on nothing
      let opacity = 1;
      for (let a = el; a; a = a.parentElement) opacity *= parseFloat(getComputedStyle(a).opacity) || 0;
      if (opacity < 0.5) return { unseen: true, control: describe(el) };
      const ring = reach(el);
      if (!ring) return { none: true, control: describe(el) };
      const r = el.getBoundingClientRect();
      const outer = Math.max(0, ring.reach);
      const shape = { left: r.left - outer, top: r.top - outer, right: r.right + outer, bottom: r.bottom + outer };
      const cuts = [], unfit = { x: false, y: false };
      for (const box of clipBoxes(el)) {
        // a control scrolled mostly out of its box is not the box's fault: it is off screen
        const shownW = Math.min(r.right, box.right) - Math.max(r.left, box.left), shownH = Math.min(r.bottom, box.bottom) - Math.max(r.top, box.top);
        if (shownW < r.width * 0.5 || shownH < r.height * 0.5) continue;
        // a control that, ring included, is larger than a scroller's clear region cannot be shown whole on that axis
        const fitsX = !box.scrolls || shape.right - shape.left <= box.clearW, fitsY = !box.scrolls || shape.bottom - shape.top <= box.clearH;
        if (!fitsX) unfit.x = true;
        if (!fitsY) unfit.y = true;
        const cut = {
          left: box.x && fitsX ? Math.max(0, box.left - shape.left) : 0, right: box.x && fitsX ? Math.max(0, shape.right - box.right) : 0,
          top: box.y && fitsY ? Math.max(0, box.top - shape.top) : 0, bottom: box.y && fitsY ? Math.max(0, shape.bottom - box.bottom) : 0,
        };
        const sides = Object.entries(cut).filter(([, v]) => v > 0.5).map(([k, v]) => k + " " + v.toFixed(1));
        if (sides.length) cuts.push({ sides, box: box.el ? describe(box.el) : { tag: "window" }, scrolls: box.scrolls });
      }
      const cs = getComputedStyle(el);
      const radius = ["borderTopLeftRadius", "borderTopRightRadius", "borderBottomRightRadius", "borderBottomLeftRadius"].map((k) => px(cs[k]));
      return { control: describe(el), ring, rect: { left: r.left, top: r.top, right: r.right, bottom: r.bottom }, shape, radius, cuts, unfit, inline: cs.display === "inline" };
    },
    tooltipOver(shape) {
      return [...document.querySelectorAll("[role=tooltip]")].some((t) => {
        const r = t.getBoundingClientRect();
        return r.width && r.right > shape.left && r.left < shape.right && r.bottom > shape.top && r.top < shape.bottom;
      });
    },
  };
})();`;

/** Share of samples on each side of the ring that show its colour anywhere across the band's thickness (give or take
 *  a pixel). Along a side only its straight run counts: a pill's ends are measured at their middle. */
export async function ringPixels(page, m, shot = {}) {
  // a 1px ring's band is one anti-aliased device pixel, and an inline box's rect spans its line fragments: geometry only
  if (m.ring.kind !== "outline" || m.ring.reach <= 0 || m.ring.width < 2 || m.inline) return null;
  const pad = 4;
  const vp = page.viewportSize();
  // whole CSS pixels: the picture's origin and scale are then exactly the clip's
  const x = Math.max(0, Math.floor(m.shape.left - pad)), y = Math.max(0, Math.floor(m.shape.top - pad));
  const clip = { x, y, width: Math.min(Math.ceil(m.shape.right + pad), vp.width) - x, height: Math.min(Math.ceil(m.shape.bottom + pad), vp.height) - y };
  if (clip.width < 4 || clip.height < 4) return null;
  const png = await page.screenshot({ clip, animations: "disabled", caret: "hide" });
  shot.png = png;
  return page.evaluate(async ({ b64, clip, m }) => {
    const img = await createImageBitmap(await (await fetch("data:image/png;base64," + b64)).blob());
    const sx = img.width / clip.width, sy = img.height / clip.height;
    const c = new OffscreenCanvas(img.width, img.height), g = c.getContext("2d", { willReadFrequently: true });
    g.drawImage(img, 0, 0);
    const data = g.getImageData(0, 0, img.width, img.height).data;
    // the colour once the screenshot finished its transitions (transition-colors moves outline-color too): measured
    // at once, a ring still fading in from currentColor would never match
    const final = document.activeElement ? getComputedStyle(document.activeElement).outlineColor : m.ring.color;
    const want = (final.match(/[\d.]+/g) || []).slice(0, 3).map(Number);
    const at = (x, y) => {
      const px = Math.floor((x - clip.x) * sx), py = Math.floor((y - clip.y) * sy);
      if (px < 0 || py < 0 || px >= img.width || py >= img.height) return false;
      const i = (py * img.width + px) * 4;
      return Math.abs(data[i] - want[0]) + Math.abs(data[i + 1] - want[1]) + Math.abs(data[i + 2] - want[2]) < 60;
    };
    const { left, top, right, bottom } = m.rect;
    const off = m.ring.offset, w = m.ring.width;
    const band = []; // distances past the border across the ring's thickness
    for (let d = off - 0.5; d <= off + w + 0.5; d += 0.5) band.push(d);
    // a radius past half the box (rounded-full's 9999px) is drawn as half the box
    const [tl, tr, br, bl] = m.radius.map((r) => Math.min(r, (bottom - top) / 2, (right - left) / 2) + off + w + 1);
    const run = (a, b) => { const out = []; for (let v = a; v <= b; v += 2) out.push(v); return out; };
    const share = (pts, hit) => Math.round((100 * pts.filter(hit).length) / pts.length);
    const midY = (top + bottom) / 2, midX = (left + right) / 2;
    const or = (xs, fallback) => (xs.length ? xs : [fallback]);
    return {
      top: share(or(run(left + tl, right - tr), midX), (x) => band.some((d) => at(x, top - d))),
      bottom: share(or(run(left + bl, right - br), midX), (x) => band.some((d) => at(x, bottom + d))),
      left: share(or(run(top + tl, bottom - bl), midY), (y) => band.some((d) => at(left - d, y))),
      right: share(or(run(top + tr, bottom - br), midY), (y) => band.some((d) => at(right + d, y))),
    };
  }, { b64: png.toString("base64"), clip, m });
}
