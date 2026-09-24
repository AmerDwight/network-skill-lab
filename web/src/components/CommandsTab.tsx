import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { getCommands } from "../api/client";
import type { CommandEntry } from "../api/types";
import { messageOf } from "../lib/errors";
import { formatTimeOfDay } from "../lib/format";

export const commandsPageSize = 100;

export const allNodes = "";

type CommandsState =
  | { status: "loading" }
  | { status: "ok"; entries: CommandEntry[]; hasMore: boolean }
  | { status: "error"; message: string };

function nodesOf(entries: CommandEntry[]): string[] {
  return [...new Set(entries.map((entry) => entry.node))].sort();
}

export function CommandsTab({ attemptId }: { attemptId: string }) {
  const { t } = useTranslation();
  const [state, setState] = useState<CommandsState>({ status: "loading" });
  const [node, setNode] = useState(allNodes);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getCommands(attemptId, { limit: commandsPageSize })
      .then((entries) => {
        if (!cancelled) {
          setState({
            status: "ok",
            entries,
            hasMore: entries.length === commandsPageSize,
          });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({ status: "error", message: messageOf(error) });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [attemptId]);

  const loadMore = useCallback(async () => {
    if (state.status !== "ok" || !state.hasMore) {
      return;
    }
    const last = state.entries.at(-1);
    if (last === undefined) {
      return;
    }
    setLoadingMore(true);
    setMoreError(null);
    try {
      const entries = await getCommands(attemptId, {
        limit: commandsPageSize,
        after: last.id,
      });
      setState({
        status: "ok",
        entries: [...state.entries, ...entries],
        hasMore: entries.length === commandsPageSize,
      });
    } catch (error) {
      setMoreError(messageOf(error));
    } finally {
      setLoadingMore(false);
    }
  }, [attemptId, state]);

  if (state.status === "loading") {
    return <p className="state">{t("commands.loading")}</p>;
  }

  if (state.status === "error") {
    return (
      <div className="banner banner--error">
        <span>{t("commands.error")}</span>
        <span className="banner__detail">{state.message}</span>
      </div>
    );
  }

  if (state.entries.length === 0) {
    return <p className="state">{t("commands.empty")}</p>;
  }

  const shown =
    node === allNodes
      ? state.entries
      : state.entries.filter((entry) => entry.node === node);

  return (
    <div className="commands">
      <label className="field field--inline" htmlFor="commands-node">
        <span className="field__label">{t("commands.node")}</span>
        <select
          id="commands-node"
          className="field__input"
          value={node}
          onChange={(event) => setNode(event.target.value)}
        >
          <option value={allNodes}>{t("commands.node.all")}</option>
          {nodesOf(state.entries).map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </label>

      <table className="commands-table" aria-label={t("commands.heading")}>
        <thead>
          <tr>
            <th scope="col">{t("commands.time")}</th>
            <th scope="col">{t("commands.node")}</th>
            <th scope="col">{t("commands.user")}</th>
            <th scope="col">{t("commands.cwd")}</th>
            <th scope="col">{t("commands.command")}</th>
            <th scope="col">{t("commands.exitCode")}</th>
          </tr>
        </thead>
        <tbody>
          {shown.map((entry) => (
            <tr key={entry.id}>
              <td className="commands-table__time">
                {formatTimeOfDay(entry.ts)}
              </td>
              <td>{entry.node}</td>
              <td>{entry.user}</td>
              <td>
                <code>{entry.cwd}</code>
              </td>
              <td>
                <code>{entry.command}</code>
              </td>
              <td
                className={
                  entry.exit_code === 0
                    ? "commands-table__exit"
                    : "commands-table__exit commands-table__exit--failed"
                }
              >
                {entry.exit_code}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {shown.length === 0 ? (
        <p className="state">{t("commands.node.empty")}</p>
      ) : null}

      {moreError !== null ? (
        <div className="banner banner--error">
          <span>{t("commands.error")}</span>
          <span className="banner__detail">{moreError}</span>
        </div>
      ) : null}

      {state.hasMore ? (
        <button
          type="button"
          className="button"
          disabled={loadingMore}
          onClick={() => void loadMore()}
        >
          {loadingMore ? t("history.loadingMore") : t("history.loadMore")}
        </button>
      ) : null}
    </div>
  );
}
