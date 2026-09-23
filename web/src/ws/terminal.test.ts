import { describe, expect, it, vi } from "vitest";

import type { SocketHandlers } from "./socket";
import {
  TerminalConnection,
  decodeOutput,
  encodeInput,
  encodeResize,
  parseTerminalMessage,
  terminalPath,
} from "./terminal";

describe("terminalPath", () => {
  it("encodes every segment", () => {
    expect(terminalPath("01J", "gw", "main")).toBe(
      "/ws/attempts/01J/term/gw/main",
    );
    expect(terminalPath("a b", "n/1", "t?")).toBe(
      "/ws/attempts/a%20b/term/n%2F1/t%3F",
    );
  });
});

describe("frame encoding", () => {
  it("encodes input as utf-8 bytes", () => {
    expect(Array.from(encodeInput("hi\r"))).toEqual([104, 105, 13]);
    expect(Array.from(encodeInput("é"))).toEqual([195, 169]);
  });

  it("decodes binary output frames", () => {
    const buffer = new Uint8Array([36, 32]).buffer;

    expect(Array.from(decodeOutput(buffer) ?? [])).toEqual([36, 32]);
    expect(Array.from(decodeOutput(new Uint8Array([7])) ?? [])).toEqual([7]);
    expect(decodeOutput("text")).toBeNull();
  });

  it("encodes the resize control message", () => {
    expect(encodeResize(120, 40)).toBe(
      '{"type":"resize","cols":120,"rows":40}',
    );
  });

  it("parses the server control messages", () => {
    expect(parseTerminalMessage('{"type":"exit"}')).toEqual({ type: "exit" });
    expect(parseTerminalMessage('{"type":"error","message":"boom"}')).toEqual({
      type: "error",
      message: "boom",
    });
    expect(parseTerminalMessage('{"type":"resize"}')).toBeNull();
    expect(parseTerminalMessage("nope")).toBeNull();
    expect(parseTerminalMessage(new ArrayBuffer(1))).toBeNull();
  });
});

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
}

function connect() {
  FakeSocket.instances = [];
  const output: number[][] = [];
  const states: string[] = [];
  const errors: string[] = [];
  const connection = new TerminalConnection({
    attemptId: "01J",
    node: "gw",
    tab: "main",
    onOutput: (bytes) => output.push(Array.from(bytes)),
    onStateChange: (state) => states.push(state),
    onError: (message) => errors.push(message),
    shouldReconnect: () => true,
    createSocket: (url, handlers) => new FakeSocket(url, handlers),
  });
  connection.start();
  FakeSocket.instances[0]?.handlers.onOpen();
  return { connection, output, states, errors };
}

describe("TerminalConnection", () => {
  it("sends input as binary and resize as text", () => {
    const session = connect();

    session.connection.send("ip a\r");
    session.connection.resize(100, 30);

    const [input, resize] = FakeSocket.instances[0]?.sent ?? [];
    expect(ArrayBuffer.isView(input)).toBe(true);
    expect(Array.from(decodeOutput(input) ?? [])).toEqual(
      Array.from(encodeInput("ip a\r")),
    );
    expect(resize).toBe('{"type":"resize","cols":100,"rows":30}');
  });

  it("passes binary frames to the terminal", () => {
    const session = connect();

    FakeSocket.instances[0]?.handlers.onMessage(
      new Uint8Array([36, 32]).buffer,
    );

    expect(session.output).toEqual([[36, 32]]);
  });

  it("stops reconnecting once the shell exits", () => {
    vi.useFakeTimers();
    const session = connect();

    FakeSocket.instances[0]?.handlers.onMessage('{"type":"exit"}');

    expect(session.states.at(-1)).toBe("exited");
    expect(FakeSocket.instances[0]?.closed).toBe(true);

    vi.advanceTimersByTime(60000);
    expect(FakeSocket.instances).toHaveLength(1);

    session.connection.restart();
    expect(FakeSocket.instances).toHaveLength(2);
    vi.useRealTimers();
  });

  it("reports server error messages", () => {
    const session = connect();

    FakeSocket.instances[0]?.handlers.onMessage(
      '{"type":"error","message":"pty is gone"}',
    );

    expect(session.errors).toEqual(["pty is gone"]);
  });

  it("reconnects after a drop while the attempt runs", () => {
    vi.useFakeTimers();
    connect();

    FakeSocket.instances[0]?.handlers.onClose();
    vi.advanceTimersByTime(1000);

    expect(FakeSocket.instances).toHaveLength(2);
    vi.useRealTimers();
  });
});
