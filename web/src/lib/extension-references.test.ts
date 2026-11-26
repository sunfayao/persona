import { describe, expect, it } from "vitest";
import { secretState } from "./extensions";

describe("secret references", () => {

  it("say whose a secret is and what it names", () => {
    expect(secretState({ name: "k", title: "K", set: true, reference: { provider: "onepassword", provider_name: "1Password", reference: "op://a/b/c", title: "GitHub token" } }))
      .toBe("来自 1Password");
  });
});
