// Pieces the two step-by-step sign-ins share (Settings › Connectors' login card in HubConnector.tsx, Settings › Channels'
// binding in Channels.tsx; docs/extensions/connectors.md "Login supports"): the QR code drawn from the hub's modules, saving it as a picture,
// asking a step again every interval until it is scanned / entered, and the note for a step this page cannot draw.
import { t } from "../../i18n";

/** A QR code from the hub's modules (rows of "0" / "1"), in the page's own colors, with its quiet zone. */
export function QRCode({ rows, size = 184, label = t("settings-loginsteps.qr_code") }: { rows: string[]; size?: number; label?: string }) {
  const n = rows.length;
  const quiet = 2;
  const cells: string[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === "1") cells.push(`M${x + quiet} ${y + quiet}h1v1h-1z`);
  });
  return (
    <svg role="img" aria-label={label} width={size} height={size} viewBox={`0 0 ${n + quiet * 2} ${n + quiet * 2}`} shapeRendering="crispEdges" className="rounded-[10px] bg-white">
      <path d={cells.join("")} fill="#000" />
    </svg>
  );
}





