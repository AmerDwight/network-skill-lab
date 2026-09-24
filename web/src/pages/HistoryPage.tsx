import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";

import type { HistoryItem, Me } from "../api/types";
import { Header } from "../components/Header";
import { formatDate, formatElapsed } from "../lib/format";
import { isTerminal } from "../lib/status";
import { useAdminStore } from "../store/admin";
import { useAuthStore } from "../store/auth";
import { useHistoryStore } from "../store/history";

function UserSelect() {
  const { t } = useTranslation();
  const users = useAdminStore((state) => state.users);
  const loadUsers = useAdminStore((state) => state.loadUsers);
  const userId = useHistoryStore((state) => state.userId);
  const selectUser = useHistoryStore((state) => state.selectUser);

  useEffect(() => {
    void loadUsers();
  }, [loadUsers]);

  return (
    <label className="field field--inline" htmlFor="history-user">
      <span className="field__label">{t("history.user")}</span>
      <select
        id="history-user"
        className="field__input"
        value={userId ?? ""}
        onChange={(event) =>
          void selectUser(event.target.value === "" ? null : event.target.value)
        }
      >
        <option value="">{t("history.user.self")}</option>
        {users.status === "ok"
          ? users.users.map((user) => (
              <option key={user.id} value={user.id}>
                {user.username}
              </option>
            ))
          : null}
      </select>
    </label>
  );
}

function TitleCell({ item, me }: { item: HistoryItem; me: Me }) {
  if (isTerminal(item.status)) {
    return (
      <Link className="history-table__link" to={`/history/${item.id}`}>
        {item.lab.title}
      </Link>
    );
  }
  if (item.user.id === me.id) {
    return (
      <Link className="history-table__link" to={`/attempts/${item.id}`}>
        {item.lab.title}
      </Link>
    );
  }
  return <span>{item.lab.title}</span>;
}

function HistoryRow({ item, me }: { item: HistoryItem; me: Me }) {
  const { t } = useTranslation();

  return (
    <tr>
      <td>
        <TitleCell item={item} me={me} />
      </td>
      <td>
        <span className="badge badge--mode">{t(`lab.mode.${item.mode}`)}</span>
      </td>
      <td>
        <span className={`badge badge--${item.status}`}>
          {t(`status.${item.status}`)}
        </span>
      </td>
      <td>{formatDate(item.created_at)}</td>
      <td>{formatElapsed(item.elapsed_ms)}</td>
      <td>{item.submit_count}</td>
      <td>{item.command_count}</td>
    </tr>
  );
}

function HistoryTable({ items, me }: { items: HistoryItem[]; me: Me }) {
  const { t } = useTranslation();

  return (
    <table className="history-table" aria-label={t("history.heading")}>
      <thead>
        <tr>
          <th scope="col">{t("history.lab")}</th>
          <th scope="col">{t("lab.modes")}</th>
          <th scope="col">{t("result.status")}</th>
          <th scope="col">{t("history.created")}</th>
          <th scope="col">{t("result.elapsed")}</th>
          <th scope="col">{t("history.submits")}</th>
          <th scope="col">{t("history.commands")}</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <HistoryRow key={item.id} item={item} me={me} />
        ))}
      </tbody>
    </table>
  );
}

function HistoryView({ me }: { me: Me }) {
  const { t } = useTranslation();
  const list = useHistoryStore((state) => state.list);
  const loadingMore = useHistoryStore((state) => state.loadingMore);
  const moreError = useHistoryStore((state) => state.moreError);
  const load = useHistoryStore((state) => state.load);
  const loadMore = useHistoryStore((state) => state.loadMore);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <>
      <h2 className="main__heading">{t("history.heading")}</h2>

      {me.role === "admin" ? <UserSelect /> : null}

      {list.status === "loading" ? (
        <p className="state">{t("history.loading")}</p>
      ) : null}

      {list.status === "error" ? (
        <div className="banner banner--error">
          <span>{t("history.error")}</span>
          <span className="banner__detail">{list.message}</span>
          <button type="button" className="button" onClick={() => void load()}>
            {t("action.retry")}
          </button>
        </div>
      ) : null}

      {list.status === "ok" && list.items.length === 0 ? (
        <p className="state">{t("history.empty")}</p>
      ) : null}

      {list.status === "ok" && list.items.length > 0 ? (
        <HistoryTable items={list.items} me={me} />
      ) : null}

      {moreError !== null ? (
        <div className="banner banner--error">
          <span>{t("history.error")}</span>
          <span className="banner__detail">{moreError}</span>
        </div>
      ) : null}

      {list.status === "ok" && list.hasMore ? (
        <button
          type="button"
          className="button"
          disabled={loadingMore}
          onClick={() => void loadMore()}
        >
          {loadingMore ? t("history.loadingMore") : t("history.loadMore")}
        </button>
      ) : null}
    </>
  );
}

export function HistoryPage() {
  const me = useAuthStore((state) => state.me);

  return (
    <div className="page">
      <Header />
      <main className="main">
        {me !== null ? <HistoryView me={me} /> : null}
      </main>
    </div>
  );
}
