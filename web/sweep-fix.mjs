// Applies TypeScript's "remove unused declaration / imports" fix-all to every project file until nothing changes.
import ts from "typescript";
import fs from "node:fs";
const cfgPath = ts.findConfigFile(".", ts.sys.fileExists, "tsconfig.json");
const cfg = ts.getParsedCommandLineOfConfigFile(cfgPath, {}, { ...ts.sys, onUnRecoverableConfigFileDiagnostic: (d) => { throw new Error(ts.flattenDiagnosticMessageText(d.messageText, "\n")); } });
const files = cfg.fileNames.filter((f) => !f.includes("node_modules"));
const versions = new Map();
const host = {
  getScriptFileNames: () => files,
  getScriptVersion: (f) => String(versions.get(f) ?? 0),
  getScriptSnapshot: (f) => (fs.existsSync(f) ? ts.ScriptSnapshot.fromString(fs.readFileSync(f, "utf8")) : undefined),
  getCurrentDirectory: () => process.cwd(),
  getCompilationSettings: () => cfg.options,
  getDefaultLibFileName: (o) => ts.getDefaultLibFilePath(o),
  fileExists: ts.sys.fileExists, readFile: ts.sys.readFile, readDirectory: ts.sys.readDirectory, directoryExists: ts.sys.directoryExists, getDirectories: ts.sys.getDirectories,
};
const ls = ts.createLanguageService(host, ts.createDocumentRegistry());
const skip = (f) => /extension-wire|todo-view-wire|video-wire|startup-preload|\/configuration\//.test(f);
let total = 0;
for (let pass = 1; pass <= 12; pass++) {
  let changed = 0;
  for (const f of files) {
    if (skip(f)) continue;
    for (const id of ["unusedIdentifier_delete", "unusedIdentifier_deleteImports"]) {
      let fix;
      try { fix = ls.getCombinedCodeFix({ type: "file", fileName: f }, id, {}, {}); } catch (e) { continue; }
      const mine = fix.changes.filter((c) => c.fileName === f);
      if (!mine.length) continue;
      let text = fs.readFileSync(f, "utf8");
      const edits = mine.flatMap((c) => c.textChanges).sort((a, b) => b.span.start - a.span.start);
      for (const e of edits) text = text.slice(0, e.span.start) + e.newText + text.slice(e.span.start + e.span.length);
      if (text !== fs.readFileSync(f, "utf8")) { fs.writeFileSync(f, text); versions.set(f, (versions.get(f) ?? 0) + 1); changed++; }
    }
  }
  console.log(`pass ${pass}: ${changed} files changed`);
  total += changed;
  if (!changed) break;
}
console.log(`total file changes ${total}`);
