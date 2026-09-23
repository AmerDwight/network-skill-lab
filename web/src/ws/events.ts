import { useEffect } from "react";

import type { AttemptEvent } from "../api/types";
import { isFinishedStatus, useAttemptStore } from "../store/attempt";

import type { ConnectionState, SocketFactory } from "./socket";
import { ReconnectingSocket, socketUrl } from "./socket";

export function eventsPath(attemptId: string): string {
  return `/ws/attempts/${encodeURIComponent(attemptId)}/events`;
}

export function parseAttemptEvent(data: unknown): AttemptEvent | null {
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
  const { type } = parsed as { type?: unknown };
  if (
    type !== "status" &&
    type !== "provisioning" &&
    type !== "checkpoint" &&
    type !== "tick" &&
    type !== "error"
  ) {
    return null;
  }
  return parsed as AttemptEvent;
}

export interface EventsConnectionOptions {
  onEvent: (event: AttemptEvent) => void;
  onStateChange: (state: ConnectionState) => void;
  onResync: () => void;
  shouldReconnect: () => boolean;
  createSocket?: SocketFactory;
}

export class EventsConnection {
  private readonly socket: ReconnectingSocket;
  private readonly options: EventsConnectionOptions;

  constructor(attemptId: string, options: EventsConnectionOptions) {
    this.options = options;
    this.socket = new ReconnectingSocket({
      url: socketUrl(eventsPath(attemptId)),
      onOpen: () => options.onResync(),
      onMessage: (data) => this.handleMessage(data),
      onStateChange: options.onStateChange,
      shouldReconnect: options.shouldReconnect,
      createSocket: options.createSocket,
    });
  }

  start(): void {
    this.socket.start();
  }

  stop(): void {
    this.socket.stop();
  }

  private handleMessage(data: unknown): void {
    const event = parseAttemptEvent(data);
    if (event === null) {
      return;
    }
    this.options.onEvent(event);
    if (event.type === "status" && isFinishedStatus(event.status)) {
      this.socket.stop();
    }
  }
}

export function useAttemptEvents(attemptId: string): void {
  useEffect(() => {
    const connection = new EventsConnection(attemptId, {
      onEvent: (event) => useAttemptStore.getState().applyEvent(event),
      onStateChange: (state) =>
        useAttemptStore.getState().setEventsConnection(state),
      onResync: () => void useAttemptStore.getState().refresh(attemptId),
      shouldReconnect: () =>
        !isFinishedStatus(useAttemptStore.getState().status),
    });
    connection.start();
    return () => connection.stop();
  }, [attemptId]);
}
