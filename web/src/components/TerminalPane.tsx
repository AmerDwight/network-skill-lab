import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAttemptStore } from "../store/attempt";
import type { TerminalState } from "../ws/terminal";
import { TerminalConnection } from "../ws/terminal";

import "@xterm/xterm/css/xterm.css";

const terminalOptions = {
  fontFamily: 'ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace',
  fontSize: 14,
  scrollback: 5000,
  cursorBlink: true,
  theme: {
    background: "#11161c",
    foreground: "#e6edf3",
    cursor: "#e6edf3",
    selectionBackground: "#2d4f6b",
  },
};

export interface TerminalPaneProps {
  attemptId: string;
  node: string;
  tab: string;
}

export function TerminalPane({ attemptId, node, tab }: TerminalPaneProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const connectionRef = useRef<TerminalConnection | null>(null);
  const [state, setState] = useState<TerminalState>("connecting");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (container === null) {
      return;
    }

    const terminal = new Terminal(terminalOptions);
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);

    const connection = new TerminalConnection({
      attemptId,
      node,
      tab,
      onOutput: (bytes) => terminal.write(bytes),
      onStateChange: (next) => {
        setState(next);
        useAttemptStore.getState().setTerminalState(`${node}:${tab}`, next);
        if (next === "open") {
          fit();
        }
      },
      onError: (message) => setError(message),
      shouldReconnect: () => {
        const status = useAttemptStore.getState().status;
        return status === "running" || status === "provisioning";
      },
    });
    connectionRef.current = connection;

    function fit() {
      if (container === null || container.clientHeight === 0) {
        return;
      }
      fitAddon.fit();
      connection.resize(terminal.cols, terminal.rows);
    }

    const dataListener = terminal.onData((data) => connection.send(data));
    const observer = new ResizeObserver(() => fit());
    observer.observe(container);
    connection.start();

    return () => {
      observer.disconnect();
      dataListener.dispose();
      connection.stop();
      connectionRef.current = null;
      terminal.dispose();
      useAttemptStore.getState().setTerminalState(`${node}:${tab}`, null);
    };
  }, [attemptId, node, tab]);

  return (
    <div className="terminal">
      <div className="terminal__surface" ref={containerRef} />
      {state === "exited" ? (
        <div className="terminal__notice">
          <span>{t("terminal.exited")}</span>
          <button
            type="button"
            className="button"
            onClick={() => {
              setError(null);
              connectionRef.current?.restart();
            }}
          >
            {t("terminal.reconnect")}
          </button>
        </div>
      ) : null}
      {error !== null ? (
        <div className="terminal__notice terminal__notice--error">
          <span>{error}</span>
        </div>
      ) : null}
      {state === "reconnecting" ? (
        <div className="terminal__overlay">{t("terminal.reconnecting")}</div>
      ) : null}
    </div>
  );
}
