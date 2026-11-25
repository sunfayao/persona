// WCAG contrast of two #rrggbb (or #rgb) colors, for the tests that keep the page's palette readable.
export const MIN_ROLE_CONTRAST = 3;

function rgb(hex: string): number[] {
  const s = hex.length === 4 ? hex.slice(1).split("").map((x) => x + x).join("") : hex.slice(1);
  return [0, 2, 4].map((i) => parseInt(s.slice(i, i + 2), 16));
}
function luminance(hex: string): number {
  return rgb(hex).map((c) => c / 255).map((v) => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
    .reduce((n, c, i) => n + c * [0.2126, 0.7152, 0.0722][i], 0);
}
export function contrast(a: string, b: string): number {
  const x = luminance(a), y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}
