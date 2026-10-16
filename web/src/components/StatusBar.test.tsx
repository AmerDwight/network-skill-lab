import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import i18next from "../i18n";

import { StatusBar } from "./StatusBar";

function renderBar(runner: string) {
  return render(
    <StatusBar
      status="running"
      elapsedMs={0}
      runner={runner}
      nodes={[{ name: "web01", role: "linux" }]}
      terminals={{}}
      eventsConnection="open"
    />,
  );
}

beforeEach(async () => {
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("StatusBar", () => {
  it("names the runner the sandbox lives on", () => {
    renderBar("lab-01");
    expect(screen.getByText("Runner: lab-01")).toBeDefined();
  });

  it("leaves the runner out until the attempt is known", () => {
    renderBar("");
    expect(screen.queryByText(/Runner:/)).toBeNull();
  });
});
