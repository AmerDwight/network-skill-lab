import type {
  TerminalResizeMessage,
  TerminalServerMessage,
} from "../api/types";

import type { ConnectionState, SocketFactory } from "./socket";
import { ReconnectingSocket, socketUrl } from "./socket";

export function terminalPath(
  attemptId: string,
  node: string,
  tab: string,
): string {
  return `/ws/attempts/${encodeURIComponent(attemptId)}/term/${encodeURIComponent(node)}/${encodeURIComponent(tab)}`;
}

const encoder = new TextEncoder();

export function encodeInput(data: string): Uint8Array {
  return encoder.encode(data);
}

export function decodeOutput(data: unknown): Uint8Array | null {
  if (data instanceof ArrayBuffer) {
    return new Uint8Array(data);
  }
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  }
  return null;
}

export function encodeResize(cols: number, rows: number): string {
  const message: TerminalResizeMessage = { type: "resize", cols, rows };
  return JSON.stringify(message);
}

export function parseTerminalMessage(
  data: unknown,
): TerminalServerMessage | null {
  if (typeof data !== "string") {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const { type, message } = parsed as { type?: unknown; message?: unknown };
  if (type === "exit") {
    return { type: "exit" };
  }
  if (type === "error") {
    return {
      type: "error",
      message: typeof message === "string" ? message : "",
    };
  }
  return null;
}

export type TerminalState = ConnectionState | "exited";

export interface TerminalConnectionOptions {
  attemptId: string;
  node: string;
  tab: string;
  onOutput: (bytes: Uint8Array) => void;
  onStateChange: (state: TerminalState) => void;
  onError: (message: string) => void;
  shouldReconnect: () => boolean;
  createSocket?: SocketFactory;
}

export class TerminalConnection {
  private readonly socket: ReconnectingSocket;
  private readonly options: TerminalConnectionOptions;
  private exited = false;

  constructor(options: TerminalConnectionOptions) {
    this.options = options;
    this.socket = new ReconnectingSocket({
      url: socketUrl(
        terminalPath(options.attemptId, options.node, options.tab),
      ),
      onOpen: () => undefined,
      onMessage: (data) => this.handleMessage(data),
      onStateChange: (state) => options.onStateChange(state),
      shouldReconnect: () => !this.exited && options.shouldReconnect(),
      createSocket: options.createSocket,
    });
  }

  start(): void {
    this.socket.start();
  }

  stop(): void {
    this.socket.stop();
  }

  restart(): void {
    this.exited = false;
    this.socket.restart();
  }

  send(data: string): void {
    this.socket.send(encodeInput(data));
  }

  resize(cols: number, rows: number): void {
    this.socket.send(encodeResize(cols, rows));
  }

  private handleMessage(data: unknown): void {
    const bytes = decodeOutput(data);
    if (bytes !== null) {
      this.options.onOutput(bytes);
      return;
    }
    const message = parseTerminalMessage(data);
    if (message === null) {
      return;
    }
    if (message.type === "exit") {
      this.exited = true;
      this.socket.stop();
      this.options.onStateChange("exited");
      return;
    }
    this.options.onError(message.message);
  }
}
