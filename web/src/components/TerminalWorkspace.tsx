import type {
  DockviewApi,
  DockviewReadyEvent,
  IDockviewPanelProps,
} from "dockview-react";
import { DockviewReact, themeDark } from "dockview-react";
import { useRef } from "react";
import { useTranslation } from "react-i18next";

import type { LabNode } from "../api/types";

import { TerminalPane } from "./TerminalPane";

import "dockview/dist/styles/dockview.css";

type TerminalPanelParams = {
  attemptId: string;
  node: string;
  tab: string;
};

function TerminalPanel(props: IDockviewPanelProps<TerminalPanelParams>) {
  return (
    <TerminalPane
      attemptId={props.params.attemptId}
      node={props.params.node}
      tab={props.params.tab}
    />
  );
}

const components = { terminal: TerminalPanel };

function panelId(node: string, tab: string): string {
  return `${node}:${tab}`;
}

function randomTabId(): string {
  return Math.random().toString(36).slice(2, 8);
}

export interface TerminalWorkspaceProps {
  attemptId: string;
  nodes: LabNode[];
}

export function TerminalWorkspace({
  attemptId,
  nodes,
}: TerminalWorkspaceProps) {
  const { t } = useTranslation();
  const apiRef = useRef<DockviewApi | null>(null);

  function handleReady(event: DockviewReadyEvent) {
    apiRef.current = event.api;
    for (const node of nodes) {
      event.api.addPanel<TerminalPanelParams>({
        id: panelId(node.name, "main"),
        component: "terminal",
        title: node.name,
        params: { attemptId, node: node.name, tab: "main" },
      });
    }
  }

  function addTab(node: string) {
    const tab = randomTabId();
    apiRef.current?.addPanel<TerminalPanelParams>({
      id: panelId(node, tab),
      component: "terminal",
      title: `${node} · ${tab}`,
      params: { attemptId, node, tab },
    });
  }

  return (
    <div className="workspace">
      <div className="workspace__toolbar">
        {nodes.map((node) => (
          <button
            key={node.name}
            type="button"
            className="button workspace__add"
            onClick={() => addTab(node.name)}
          >
            {t("terminal.addTab", { node: node.name })}
          </button>
        ))}
      </div>
      <DockviewReact
        className="workspace__dock"
        components={components}
        theme={themeDark}
        onReady={handleReady}
      />
    </div>
  );
}
