import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AttemptEvent } from "../api/types";

import { EventsConnection, eventsPath, parseAttemptEvent } from "./events";
import type { ConnectionState, SocketHandlers } from "./socket";

class FakeSocket {
  static instances: FakeSocket[] = [];
  readonly sent: (string | ArrayBufferLike | ArrayBufferView)[] = [];
  closed = false;

  constructor(
    readonly url: string,
    readonly handlers: SocketHandlers,
  ) {
    FakeSocket.instances.push(this);
  }

  send(data: string | ArrayBufferLike | ArrayBufferView): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
  }

  open(): void {
    this.handlers.onOpen();
  }

  emit(message: unknown): void {
    this.handlers.onMessage(JSON.stringify(message));
  }

  drop(): void {
    this.closed = true;
    this.handlers.onClose();
  }
}

function connect(shouldReconnect: () => boolean) {
  const events: AttemptEvent[] = [];
  const states: ConnectionState[] = [];
  let resyncs = 0;
  const connection = new EventsConnection("01JATTEMPT", {
    onEvent: (event) => events.push(event),
    onStateChange: (state) => states.push(state),
    onResync: () => {
      resyncs += 1;
    },
    shouldReconnect,
    createSocket: (url, handlers) => new FakeSocket(url, handlers),
  });
  connection.start();
  return { connection, events, states, resyncs: () => resyncs };
}

beforeEach(() => {
  FakeSocket.instances = [];
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("eventsPath", () => {
  it("encodes the attempt id", () => {
    expect(eventsPath("a/b")).toBe("/ws/attempts/a%2Fb/events");
  });
});

describe("parseAttemptEvent", () => {
  it("accepts every message of the contract", () => {
    expect(
      parseAttemptEvent('{"type":"tick","elapsed_ms":1,"server_time":"t"}'),
    ).toEqual({ type: "tick", elapsed_ms: 1, server_time: "t" });
  });

  it("rejects anything else", () => {
    expect(parseAttemptEvent("not json")).toBeNull();
    expect(parseAttemptEvent('{"type":"nope"}')).toBeNull();
    expect(parseAttemptEvent(new ArrayBuffer(2))).toBeNull();
  });
});

describe("EventsConnection", () => {
  it("resyncs after every connect and dispatches events", () => {
    const session = connect(() => true);

    FakeSocket.instances[0]?.open();
    FakeSocket.instances[0]?.emit({
      type: "checkpoint",
      id: "link-up",
      status: "pass",
      first_passed_at: null,
    });

    expect(session.resyncs()).toBe(1);
    expect(session.events).toHaveLength(1);
    expect(session.states).toEqual(["connecting", "open"]);
  });

  it("reconnects with a doubling backoff capped at 30 s", () => {
    const timer = vi.spyOn(globalThis, "setTimeout");
    connect(() => true);

    const delays: number[] = [];
    for (let attempt = 0; attempt < 7; attempt += 1) {
      const before = FakeSocket.instances.length;
      FakeSocket.instances.at(-1)?.drop();
      delays.push(Number(timer.mock.calls.at(-1)?.[1]));
      vi.advanceTimersToNextTimer();
      expect(FakeSocket.instances).toHaveLength(before + 1);
    }

    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
    timer.mockRestore();
  });

  it("resets the backoff after a successful connect", () => {
    connect(() => true);

    FakeSocket.instances[0]?.drop();
    vi.advanceTimersByTime(1000);
    FakeSocket.instances[1]?.open();
    FakeSocket.instances[1]?.drop();
    vi.advanceTimersByTime(999);
    expect(FakeSocket.instances).toHaveLength(2);

    vi.advanceTimersByTime(1);
    expect(FakeSocket.instances).toHaveLength(3);
  });

  it("stops once the attempt reaches a terminal status", () => {
    const session = connect(() => true);

    FakeSocket.instances[0]?.open();
    FakeSocket.instances[0]?.emit({
      type: "status",
      status: "passed",
      error_message: "",
      elapsed_ms: 1000,
      server_time: "t",
    });

    expect(FakeSocket.instances[0]?.closed).toBe(true);
    expect(session.states.at(-1)).toBe("closed");

    FakeSocket.instances[0]?.handlers.onClose();
    vi.advanceTimersByTime(60000);
    expect(FakeSocket.instances).toHaveLength(1);
  });

  it("does not reconnect when the caller says the attempt is over", () => {
    connect(() => false);

    FakeSocket.instances[0]?.drop();
    vi.advanceTimersByTime(60000);

    expect(FakeSocket.instances).toHaveLength(1);
  });

  it("stops on unmount", () => {
    const session = connect(() => true);

    session.connection.stop();
    vi.advanceTimersByTime(60000);

    expect(FakeSocket.instances[0]?.closed).toBe(true);
    expect(FakeSocket.instances).toHaveLength(1);
  });
});
