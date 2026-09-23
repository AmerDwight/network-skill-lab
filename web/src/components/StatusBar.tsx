import { useTranslation } from "react-i18next";

import type { AttemptStatus, LabNode } from "../api/types";
import type { ConnectionState } from "../ws/socket";
import type { TerminalState } from "../ws/terminal";

export function formatElapsed(milliseconds: number): string {
  const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

function nodeConnection(
  terminals: Record<string, TerminalState>,
  node: string,
): TerminalState | null {
  const states = Object.entries(terminals)
    .filter(([key]) => key.startsWith(`${node}:`))
    .map(([, state]) => state);
  if (states.length === 0) {
    return null;
  }
  return states.includes("open") ? "open" : states[0];
}

export interface StatusBarProps {
  status: AttemptStatus | null;
  elapsedMs: number;
  nodes: LabNode[];
  terminals: Record<string, TerminalState>;
  eventsConnection: ConnectionState;
}

export function StatusBar({
  status,
  elapsedMs,
  nodes,
  terminals,
  eventsConnection,
}: StatusBarProps) {
  const { t } = useTranslation();

  return (
    <footer className="status-bar">
      <span className="status-bar__timer">{formatElapsed(elapsedMs)}</span>
      <span className={`badge badge--${status ?? "unknown"}`}>
        {status === null ? t("status.unknown") : t(`status.${status}`)}
      </span>
      <ul className="status-bar__nodes">
        {nodes.map((node) => {
          const connection = nodeConnection(terminals, node.name);
          return (
            <li key={node.name} className="status-bar__node">
              <span
                className={`dot dot--${connection ?? "idle"}`}
                aria-label={
                  connection === null
                    ? t("connection.idle")
                    : t(`connection.${connection}`)
                }
              />
              {node.name}
            </li>
          );
        })}
      </ul>
      <span className="status-bar__events">
        <span className={`dot dot--${eventsConnection}`} />
        {t("connection.events")}: {t(`connection.${eventsConnection}`)}
      </span>
    </footer>
  );
}
