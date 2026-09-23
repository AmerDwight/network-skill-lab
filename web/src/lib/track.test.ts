import { describe, expect, it } from "vitest";

import type { Track, TrackStep } from "../api/types";

import { stepHref } from "./paths";
import { completedSteps, nextStep } from "./track";

function step(overrides: Partial<TrackStep> = {}): TrackStep {
  return {
    kind: "doc",
    ref: "net/ip/guide",
    title: "IP guide",
    mode: null,
    completed: false,
    ...overrides,
  };
}

function track(steps: TrackStep[]): Track {
  return { id: "network-basics", title: "Network basics", steps };
}

describe("nextStep", () => {
  it("is the first step that is not completed", () => {
    const target = step({ ref: "net/dns/guide" });

    expect(nextStep(track([step({ completed: true }), target]))).toBe(target);
  });

  it("is null once every step is completed", () => {
    expect(nextStep(track([step({ completed: true })]))).toBeNull();
    expect(nextStep(track([]))).toBeNull();
  });
});

describe("completedSteps", () => {
  it("counts the completed steps", () => {
    expect(completedSteps(track([step({ completed: true }), step()]))).toBe(1);
  });
});

describe("stepHref", () => {
  it("points a doc step at the doc page", () => {
    expect(stepHref(step())).toBe("/docs/net/ip/guide");
  });

  it("carries the mode of a lab step", () => {
    expect(
      stepHref(step({ kind: "lab", ref: "net-ip-01", mode: "tutorial" })),
    ).toBe("/labs/net-ip-01?mode=tutorial");
  });

  it("leaves the mode out when the lab step has none", () => {
    expect(stepHref(step({ kind: "lab", ref: "net-ip-01" }))).toBe(
      "/labs/net-ip-01",
    );
  });
});
