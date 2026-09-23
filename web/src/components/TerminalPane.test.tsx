import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import i18next from "../i18n";
import { initialState, useAttemptStore } from "../store/attempt";

import { TerminalPane } from "./TerminalPane";

const write = vi.fn();
const dispose = vi.fn();
const fit = vi.fn();

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    write = write;
    dispose = dispose;
    loadAddon() {}
    open() {}
    onData() {
      return { dispose: () => undefined };
    }
  },
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = fit;
  },
}));

vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(): void {}

  close(): void {
    this.onclose?.();
  }
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  useAttemptStore.setState(initialState);
  vi.stubGlobal("WebSocket", FakeWebSocket);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("TerminalPane", () => {
  it("opens one socket for the node and tab", () => {
    render(<TerminalPane attemptId="01J" node="gw" tab="main" />);

    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(FakeWebSocket.instances[0]?.url).toContain(
      "/ws/attempts/01J/term/gw/main",
    );
    expect(FakeWebSocket.instances[0]?.binaryType).toBe("arraybuffer");
  });

  it("writes server output into the terminal", () => {
    render(<TerminalPane attemptId="01J" node="gw" tab="main" />);

    const socket = FakeWebSocket.instances[0];
    socket?.onopen?.();
    socket?.onmessage?.({ data: new Uint8Array([36, 32]).buffer });

    expect(write).toHaveBeenCalledOnce();
    expect(useAttemptStore.getState().terminals["gw:main"]).toBe("open");
  });

  it("offers a reconnect button once the shell exits", async () => {
    render(<TerminalPane attemptId="01J" node="gw" tab="main" />);

    const socket = FakeWebSocket.instances[0];
    socket?.onopen?.();
    socket?.onmessage?.({ data: '{"type":"exit"}' });

    expect(await screen.findByText(i18next.t("terminal.exited"))).toBeDefined();
  });

  it("disposes the terminal on unmount", () => {
    const view = render(<TerminalPane attemptId="01J" node="gw" tab="main" />);

    view.unmount();

    expect(dispose).toHaveBeenCalled();
    expect(useAttemptStore.getState().terminals["gw:main"]).toBeUndefined();
  });
});
