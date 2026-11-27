/// <reference types="node" />
// The to-do strip's one line is part of the chat page; its details, their criteria marks and their words load on the
// first open (TodoStrip.tsx).
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "../../i18n/startup-namespaces.json";

const read = (file: string) => readFileSync(new URL(file, import.meta.url), "utf8");
const staticImport = (name: string) => new RegExp(`^import\\s+(?!type\\b)[^;]*from\\s+["'][./]+(chat/)?${name}["']`, "m");

describe("the to-do strip boundary", () => {
  it("keeps the details out of the strip's and the chat's static imports, and their words out of startup", () => {
    for (const file of ["./TodoStrip.tsx", "./ChatView.tsx"]) {
      expect(read(file), file).not.toMatch(staticImport("TodoStripDetail"));
      expect(read(file), file).not.toMatch(staticImport("TodoCriterionMark"));
    }
    expect(read("./TodoStrip.tsx")).toMatch(/lazy\(\(\) => import\("\.\/TodoStripDetail"\)/);
    expect(read("./ChatView.tsx")).toMatch(/from "\.\/TodoStrip";/);
    expect(startup).toContain("chat-todostrip");
    expect(startup).not.toContain("chat-todostripdetail");
  });
});
