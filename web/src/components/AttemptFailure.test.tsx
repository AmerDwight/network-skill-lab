import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";

import i18next from "../i18n";

import { AttemptFailure, runnerDisconnected } from "./AttemptFailure";

function renderFailure(message: string) {
  return render(
    <MemoryRouter>
      <AttemptFailure message={message} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
});

describe("AttemptFailure", () => {
  it("explains a runner disconnect in each language", async () => {
    await i18next.changeLanguage("en");
    renderFailure(runnerDisconnected);
    expect(
      screen.getByText(
        "The runner hosting this sandbox disconnected, so the attempt ended. Start the lab again to get a new sandbox.",
      ),
    ).toBeDefined();
    cleanup();

    await i18next.changeLanguage("zh-TW");
    renderFailure(runnerDisconnected);
    expect(
      screen.getByText(
        "承載這個沙箱的 runner 斷線了，這次練習已結束。重新開始即可取得新的沙箱。",
      ),
    ).toBeDefined();
  });

  it("shows any other reason as the server wrote it", async () => {
    await i18next.changeLanguage("en");
    renderFailure("bootstrap: node web01 exited with 1");
    expect(
      screen.getByText("bootstrap: node web01 exited with 1"),
    ).toBeDefined();
  });
});
