import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { setShowsSampleRing, showsSampleRing } from "./palette";

describe("eyedropper sample-ring preference", () => {
  beforeEach(() => {
    showsSampleRing.set(true);
  });

  it("defaults on and only accepts boolean updates", () => {
    expect(get(showsSampleRing)).toBe(true);

    setShowsSampleRing(false);
    expect(get(showsSampleRing)).toBe(false);

    setShowsSampleRing("off" as unknown as boolean);
    expect(get(showsSampleRing)).toBe(false);

    setShowsSampleRing(true);
    expect(get(showsSampleRing)).toBe(true);
  });
});
