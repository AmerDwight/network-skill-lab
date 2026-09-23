import { describe, expect, it } from "vitest";

import type { AttemptCheckpoint, TutorialStep } from "../api/types";

import { currentStepIndex, tutorialStepViews } from "./tutorial";

const steps: TutorialStep[] = [
  { checkpoint: "link-up", instruction: "bring the link up" },
  { checkpoint: "ping-ok", instruction: "ping the gateway" },
];

function checkpoint(
  id: string,
  status: AttemptCheckpoint["status"],
): AttemptCheckpoint {
  return { id, title: `title of ${id}`, status, first_passed_at: null };
}

function byId(
  ...items: AttemptCheckpoint[]
): Record<string, AttemptCheckpoint> {
  return Object.fromEntries(items.map((item) => [item.id, item]));
}

describe("tutorialStepViews", () => {
  it("joins each step with its checkpoint", () => {
    const views = tutorialStepViews(
      steps,
      byId(checkpoint("link-up", "pass"), checkpoint("ping-ok", "fail")),
    );

    expect(views).toEqual([
      {
        checkpoint: "link-up",
        instruction: "bring the link up",
        title: "title of link-up",
        status: "pass",
      },
      {
        checkpoint: "ping-ok",
        instruction: "ping the gateway",
        title: "title of ping-ok",
        status: "fail",
      },
    ]);
  });

  it("falls back to the checkpoint id while nothing is known", () => {
    const views = tutorialStepViews(steps, {});

    expect(views[0]).toEqual({
      checkpoint: "link-up",
      instruction: "bring the link up",
      title: "link-up",
      status: "pending",
    });
  });
});

describe("currentStepIndex", () => {
  it("is the first step that has not passed", () => {
    expect(currentStepIndex(tutorialStepViews(steps, {}))).toBe(0);
    expect(
      currentStepIndex(
        tutorialStepViews(steps, byId(checkpoint("link-up", "pass"))),
      ),
    ).toBe(1);
  });

  it("skips back to an earlier step passed out of order", () => {
    expect(
      currentStepIndex(
        tutorialStepViews(steps, byId(checkpoint("ping-ok", "pass"))),
      ),
    ).toBe(0);
  });

  it("is -1 once every step has passed", () => {
    expect(
      currentStepIndex(
        tutorialStepViews(
          steps,
          byId(checkpoint("link-up", "pass"), checkpoint("ping-ok", "pass")),
        ),
      ),
    ).toBe(-1);
  });
});
