// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "./api";
import { clearConversationDeletions, rememberConversationDeleted } from "./conversation-deleted";
import {
  hasDraftImages,
  MAX_SESSION_DRAFT_IMAGE_CHARS,
  MAX_STORED_DRAFT_IMAGE_CONVERSATIONS,
  onDraftEvent,
  storeDraft,
  storedDraft,
  storeDraftImages,
  storedDraftImages,
  useLeftDraft,
  type StoredDraftImage,
} from "./drafts";
import { noEdits, noHistory } from "./image-edit";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const sampleImage = (name: string, payload = "png-bytes"): StoredDraftImage => ({
  id: `id-${name}`,
  name,
  mime: "image/png",
  data: btoa(payload),
});

beforeEach(() => {
  clearConversationDeletions();
  localStorage.clear();
  sessionStorage.clear();
  setToken("drafts-test-owner");
});

afterEach(() => {
  clearConversationDeletions();
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("per-conversation draft image persistence", () => {
  it("stores, reads, updates LRU order, and clears draft images per conversation", () => {
    const imgA = sampleImage("a.png", "alpha");
    const imgB = sampleImage("b.png", "beta");

    expect(storedDraftImages("mac/s1")).toEqual([]);
    expect(hasDraftImages("mac/s1")).toBe(false);

    storeDraftImages("mac/s1", [imgA]);
    storeDraftImages("mac/s2", [imgB]);

    expect(hasDraftImages("mac/s1")).toBe(true);
    expect(storedDraftImages("mac/s1")).toEqual([imgA]);
    expect(storedDraftImages("mac/s2")).toEqual([imgB]);

    storeDraftImages("mac/s1", []);
    expect(hasDraftImages("mac/s1")).toBe(false);
    expect(storedDraftImages("mac/s1")).toEqual([]);
    expect(storedDraftImages("mac/s2")).toEqual([imgB]);
  });

  it("preserves in-memory EditedImage metadata across conversation switches", () => {
    const editedImg: StoredDraftImage = {
      id: "edited-1",
      name: "annotated.png",
      mime: "image/png",
      data: btoa("cropped"),
      edited: { source: "blob:test/original", edits: { ...noEdits(), turns: 1 }, history: noHistory() },
      editedSource: { mime: "image/png", data: btoa("original") },
    };

    storeDraftImages("mac/edit", [editedImg]);
    const restored = storedDraftImages("mac/edit");
    expect(restored).toEqual([editedImg]);
  });

  it("evicts oldest unmounted conversations past MAX_STORED_DRAFT_IMAGE_CONVERSATIONS while protecting mounted keys", () => {
    const offMounted = onDraftEvent("mac/mounted", () => {});
    try {
      storeDraftImages("mac/mounted", [sampleImage("mounted.png")]);
      for (let i = 0; i < MAX_STORED_DRAFT_IMAGE_CONVERSATIONS + 3; i++) {
        storeDraftImages(`mac/unmounted-${i}`, [sampleImage(`u-${i}.png`)]);
      }

      // Mounted key survives even though it was inserted earliest.
      expect(hasDraftImages("mac/mounted")).toBe(true);
      // Oldest unmounted conversations were evicted first.
      expect(hasDraftImages("mac/unmounted-0")).toBe(false);
      expect(hasDraftImages("mac/unmounted-1")).toBe(false);
      expect(hasDraftImages("mac/unmounted-2")).toBe(false);
      expect(hasDraftImages("mac/unmounted-3")).toBe(false);
      // Most recent unmounted conversations remain.
      expect(hasDraftImages(`mac/unmounted-${MAX_STORED_DRAFT_IMAGE_CONVERSATIONS + 2}`)).toBe(true);
    } finally {
      offMounted();
    }
  });

  it("mirrors small/medium screenshots to sessionStorage and skips oversized payloads without dropping in-memory state", () => {
    const small = sampleImage("shot.png", "small-screenshot");
    storeDraftImages("mac/reload", [small]);
    expect(sessionStorage.getItem("draft-images:mac/reload")).toContain(small.data);

    const huge: StoredDraftImage = {
      id: "huge-1",
      name: "huge.png",
      mime: "image/png",
      data: "A".repeat(MAX_SESSION_DRAFT_IMAGE_CHARS + 16),
    };
    storeDraftImages("mac/reload", [huge]);
    // Oversized payload is removed from sessionStorage so reload never reads stale data, while memory keeps 100%.
    expect(sessionStorage.getItem("draft-images:mac/reload")).toBeNull();
    expect(storedDraftImages("mac/reload")).toHaveLength(1);
    expect(storedDraftImages("mac/reload")[0].data.length).toBe(MAX_SESSION_DRAFT_IMAGE_CHARS + 16);
  });

  it("fences draft images across conversation deletion, scope reset, and credential changes", () => {
    storeDraftImages("mac/del", [sampleImage("secret.png")]);
    expect(hasDraftImages("mac/del")).toBe(true);

    rememberConversationDeleted("mac", "del");
    expect(hasDraftImages("mac/del")).toBe(false);
    expect(storedDraftImages("mac/del")).toEqual([]);
    storeDraftImages("mac/del", [sampleImage("late.png")]);
    expect(storedDraftImages("mac/del")).toEqual([]);

    storeDraftImages("mac/acct", [sampleImage("owner1.png")]);
    expect(hasDraftImages("mac/acct")).toBe(true);
    setToken("drafts-other-owner");
    expect(hasDraftImages("mac/acct")).toBe(false);
    expect(storedDraftImages("mac/acct")).toEqual([]);
  });
});

describe("in-memory text draft fallback on QuotaExceededError", () => {
  it("retains and updates drafts in memory when localStorage.setItem throws QuotaExceededError", () => {
    storeDraft("quota/chat", "initial on disk");
    expect(localStorage.getItem("draft:quota/chat")).toBe("initial on disk");

    const quotaErr = Object.assign(new Error("QuotaExceededError"), { name: "QuotaExceededError" });
    const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw quotaErr;
    });
    try {
      storeDraft("quota/chat", "updated while full");
      // Target key is protected from self-eviction on disk, and storedDraft serves the latest in-memory text.
      expect(localStorage.getItem("draft:quota/chat")).toBe("initial on disk");
      expect(storedDraft("quota/chat")).toBe("updated while full");

      storeDraft("quota/fresh", "typed in full storage");
      // Even after safeDraftWrite evicts older unmounted disk entries while retrying, both drafts survive in memory.
      expect(localStorage.getItem("draft:quota/fresh")).toBeNull();
      expect(storedDraft("quota/chat")).toBe("updated while full");
      expect(storedDraft("quota/fresh")).toBe("typed in full storage");

      // Clearing a draft when sending clears the in-memory copy too.
      storeDraft("quota/fresh", "");
      expect(storedDraft("quota/fresh")).toBe("");
    } finally {
      spy.mockRestore();
      storeDraft("quota/chat", "");
      storeDraft("quota/fresh", "");
    }
  });

  it("fences in-memory fallback drafts across credential switches and conversation deletion", () => {
    const quotaErr = Object.assign(new Error("QuotaExceededError"), { name: "QuotaExceededError" });
    const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw quotaErr;
    });
    try {
      storeDraft("quota/private", "secret draft");
      expect(storedDraft("quota/private")).toBe("secret draft");

      rememberConversationDeleted("quota", "private");
      expect(storedDraft("quota/private")).toBe("");

      storeDraft("quota/owner", "owner-1 draft");
      expect(storedDraft("quota/owner")).toBe("owner-1 draft");
    } finally {
      spy.mockRestore();
    }
    setToken("another-owner");
    expect(storedDraft("quota/owner")).toBe("");
  });
});

describe("useLeftDraft", () => {
  let host: HTMLDivElement;
  let root: Root;
  let observed: { text: string; quoted: boolean } = { text: "", quoted: false };

  function Probe({ draftKey }: { draftKey: string | null }) {
    observed = useLeftDraft(draftKey);
    return null;
  }

  beforeEach(() => {
    observed = { text: "", quoted: false };
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  it("preserves the exact { text, quoted } shape and surfaces unmounted image-only and quota-fallback drafts", () => {
    act(() => root.render(createElement(Probe, { draftKey: "mac/row" })));
    expect(observed).toEqual({ text: "", quoted: false });

    // Image-only draft marks quoted: true when unmounted, false while mounted.
    act(() => storeDraftImages("mac/row", [sampleImage("only.png")]));
    expect(observed).toEqual({ text: "", quoted: true });

    let off!: () => void;
    act(() => { off = onDraftEvent("mac/row", () => {}); });
    expect(observed).toEqual({ text: "", quoted: false });
    act(() => off());
    expect(observed).toEqual({ text: "", quoted: true });

    act(() => storeDraftImages("mac/row", []));
    expect(observed).toEqual({ text: "", quoted: false });
  });
});
