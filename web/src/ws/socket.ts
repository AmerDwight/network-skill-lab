export type ConnectionState = "connecting" | "reconnecting" | "open" | "closed";

export interface Socket {
  send: (data: string | ArrayBufferLike | ArrayBufferView) => void;
  close: () => void;
}

export interface SocketHandlers {
  onOpen: () => void;
  onMessage: (data: unknown) => void;
  onClose: () => void;
}

export type SocketFactory = (url: string, handlers: SocketHandlers) => Socket;

export function socketUrl(path: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
}

export const browserSocketFactory: SocketFactory = (url, handlers) => {
  const socket = new WebSocket(url);
  socket.binaryType = "arraybuffer";
  socket.onopen = () => handlers.onOpen();
  socket.onmessage = (event: MessageEvent<unknown>) =>
    handlers.onMessage(event.data);
  socket.onclose = () => handlers.onClose();
  socket.onerror = () => socket.close();
  return socket;
};

export const initialBackoffMs = 1000;

export const maxBackoffMs = 30000;

export function backoffDelay(failures: number): number {
  return Math.min(initialBackoffMs * 2 ** failures, maxBackoffMs);
}

export interface ReconnectingSocketOptions {
  url: string;
  onOpen: () => void;
  onMessage: (data: unknown) => void;
  onStateChange: (state: ConnectionState) => void;
  shouldReconnect: () => boolean;
  createSocket?: SocketFactory;
}

export class ReconnectingSocket {
  private readonly options: ReconnectingSocketOptions;
  private socket: Socket | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private failures = 0;
  private stopped = true;
  private open = false;

  constructor(options: ReconnectingSocketOptions) {
    this.options = options;
  }

  get isOpen(): boolean {
    return this.open;
  }

  start(): void {
    if (!this.stopped) {
      return;
    }
    this.stopped = false;
    this.failures = 0;
    this.connect("connecting");
  }

  stop(): void {
    this.stopped = true;
    this.open = false;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    const socket = this.socket;
    this.socket = null;
    socket?.close();
    this.options.onStateChange("closed");
  }

  restart(): void {
    this.stop();
    this.start();
  }

  send(data: string | ArrayBufferLike | ArrayBufferView): void {
    if (this.open) {
      this.socket?.send(data);
    }
  }

  private connect(state: ConnectionState): void {
    this.options.onStateChange(state);
    const createSocket = this.options.createSocket ?? browserSocketFactory;
    this.socket = createSocket(this.options.url, {
      onOpen: () => {
        this.failures = 0;
        this.open = true;
        this.options.onStateChange("open");
        this.options.onOpen();
      },
      onMessage: (data) => this.options.onMessage(data),
      onClose: () => this.handleClose(),
    });
  }

  private handleClose(): void {
    this.socket = null;
    this.open = false;
    if (this.stopped) {
      return;
    }
    if (!this.options.shouldReconnect()) {
      this.stop();
      return;
    }
    const delay = backoffDelay(this.failures);
    this.failures += 1;
    this.options.onStateChange("reconnecting");
    this.timer = setTimeout(() => {
      this.timer = null;
      if (!this.stopped) {
        this.connect("reconnecting");
      }
    }, delay);
  }
}
