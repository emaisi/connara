import { describe, expect, it } from "vitest";
import { entries } from "../policy-input";

describe("entries", () => {
  it("normalizes comma and newline separated policy entries", () => {
    expect(entries(" github.* , github.read\ngithub.*\n")).toEqual(["github.*", "github.read"]);
  });
});
