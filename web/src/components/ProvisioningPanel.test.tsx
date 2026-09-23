import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import i18next from "../i18n";

import { ProvisioningPanel } from "./ProvisioningPanel";

const linuxNodes = [{ name: "host", role: "linux" }];
const k3sNodes = [{ name: "server", role: "k3s-server" }];

function labels(): string[] {
  return screen
    .getAllByRole("listitem")
    .map((item) => item.textContent ?? "")
    .filter((text) => text !== "");
}

beforeEach(async () => {
  await i18next.changeLanguage("en");
});

afterEach(() => {
  cleanup();
});

describe("ProvisioningPanel", () => {
  it("shows neither k3s nor precheck for a plain lab", () => {
    render(
      <ProvisioningPanel
        step="containers"
        nodes={linuxNodes}
        precheckAttempt={null}
      />,
    );

    expect(labels()).toEqual([
      "Creating networks",
      "Starting nodes",
      "Bootstrapping",
      "Running setup",
    ]);
  });

  it("shows k3s for a lab with a k3s node", () => {
    render(
      <ProvisioningPanel
        step="bootstrap"
        nodes={k3sNodes}
        precheckAttempt={null}
      />,
    );

    expect(labels()).toContain("Waiting for k3s");
  });

  it("shows k3s once a k3s step arrives for an unknown lab", () => {
    render(<ProvisioningPanel step="k3s" nodes={[]} precheckAttempt={null} />);

    expect(labels()).toContain("Waiting for k3s");
  });

  it("shows the precheck step once a precheck message arrives", () => {
    render(
      <ProvisioningPanel
        step="precheck"
        nodes={linuxNodes}
        precheckAttempt={1}
      />,
    );

    expect(labels()).toContain("Checking the sandbox");
    expect(
      screen.getByText("Checking the sandbox").getAttribute("aria-current"),
    ).toBe("step");
  });

  it("numbers the precheck step on a retry", () => {
    render(
      <ProvisioningPanel
        step="precheck"
        nodes={linuxNodes}
        precheckAttempt={3}
      />,
    );

    expect(labels()).toContain("Checking the sandbox (attempt 3)");
  });

  it("highlights the containers step again while a retry provisions", () => {
    render(
      <ProvisioningPanel
        step="containers"
        nodes={linuxNodes}
        precheckAttempt={1}
      />,
    );

    expect(
      screen.getByText("Starting nodes").getAttribute("aria-current"),
    ).toBe("step");
    expect(labels()).toContain("Checking the sandbox");
  });
});
