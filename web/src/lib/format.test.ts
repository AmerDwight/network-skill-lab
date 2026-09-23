import { describe, expect, it } from "vitest";

import { checkpointIcons, formatElapsed, formatTimeOfDay } from "./format";

describe("formatElapsed", () => {
  it("renders minutes and seconds with two digits", () => {
    expect(formatElapsed(0)).toBe("00:00");
    expect(formatElapsed(9000)).toBe("00:09");
    expect(formatElapsed(65000)).toBe("01:05");
    expect(formatElapsed(254000)).toBe("04:14");
  });

  it("truncates sub-second remainders", () => {
    expect(formatElapsed(1999)).toBe("00:01");
  });

  it("keeps counting past an hour", () => {
    expect(formatElapsed(3_600_000)).toBe("60:00");
  });

  it("clamps negative input", () => {
    expect(formatElapsed(-5000)).toBe("00:00");
  });
});

describe("formatTimeOfDay", () => {
  it("returns null without a timestamp", () => {
    expect(formatTimeOfDay(null)).toBeNull();
  });

  it("renders a timestamp in the local time zone", () => {
    const value = "2026-09-23T08:15:30.000Z";

    expect(formatTimeOfDay(value)).toBe(new Date(value).toLocaleTimeString());
  });

  it("passes through a value it cannot parse", () => {
    expect(formatTimeOfDay("not a time")).toBe("not a time");
  });
});

describe("checkpointIcons", () => {
  it("has an icon for every checkpoint status", () => {
    expect(Object.keys(checkpointIcons).sort()).toEqual([
      "error",
      "fail",
      "pass",
      "pending",
    ]);
  });
});
