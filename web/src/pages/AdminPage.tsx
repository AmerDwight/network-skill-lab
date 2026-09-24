import type { FormEvent } from "react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { AdminAttempt, AdminStats, AdminUser, Me } from "../api/types";
import { Header } from "../components/Header";
import { formatBytes, formatDate, formatElapsed } from "../lib/format";
import { isValidPassword, isValidUsername } from "../lib/validation";
import { useAdminStore } from "../store/admin";
import { useAuthStore } from "../store/auth";

function StatsStrip({ stats }: { stats: AdminStats }) {
  const { t } = useTranslation();

  return (
    <dl className="admin-stats">
      <div className="admin-stats__item">
        <dt>{t("admin.stats.sandboxes")}</dt>
        <dd>
          {t("admin.stats.sandboxes.value", {
            active: stats.sandboxes_active,
            max: stats.sandboxes_max,
          })}
        </dd>
      </div>
      <div className="admin-stats__item">
        <dt>{t("admin.stats.recordings")}</dt>
        <dd>{formatBytes(stats.recordings_bytes)}</dd>
      </div>
      <div className="admin-stats__item">
        <dt>{t("admin.stats.attempts")}</dt>
        <dd>{stats.attempts}</dd>
      </div>
    </dl>
  );
}

function CreateUserForm({ onInvalid }: { onInvalid: (key: string) => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<"admin" | "user">("user");
  const pending = useAdminStore((state) => state.pending);
  const createUser = useAdminStore((state) => state.createUser);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!isValidUsername(username)) {
      onInvalid("admin.error.username");
      return;
    }
    if (!isValidPassword(password)) {
      onInvalid("admin.error.password");
      return;
    }
    if (await createUser({ username, password, role })) {
      setUsername("");
      setPassword("");
      setRole("user");
    }
  }

  return (
    <form
      className="admin-form"
      onSubmit={(event) => void handleSubmit(event)}
      aria-label={t("admin.create.heading")}
    >
      <label className="field" htmlFor="create-username">
        <span className="field__label">{t("admin.user.username")}</span>
        <input
          id="create-username"
          className="field__input"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
        />
      </label>

      <label className="field" htmlFor="create-password">
        <span className="field__label">{t("login.password")}</span>
        <input
          id="create-password"
          className="field__input"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </label>

      <label className="field" htmlFor="create-role">
        <span className="field__label">{t("admin.user.role")}</span>
        <select
          id="create-role"
          className="field__input"
          value={role}
          onChange={(event) =>
            setRole(event.target.value === "admin" ? "admin" : "user")
          }
        >
          <option value="user">{t("role.user")}</option>
          <option value="admin">{t("role.admin")}</option>
        </select>
      </label>

      <button
        type="submit"
        className="button button--primary"
        disabled={pending === "create"}
      >
        {t("admin.create.submit")}
      </button>
    </form>
  );
}

function UserRow({
  user,
  self,
  onInvalid,
}: {
  user: AdminUser;
  self: boolean;
  onInvalid: (key: string) => void;
}) {
  const { t } = useTranslation();
  const pending = useAdminStore((state) => state.pending);
  const updateUser = useAdminStore((state) => state.updateUser);
  const busy = self || pending === user.id;

  async function handleResetPassword() {
    const password = window.prompt(t("admin.resetPassword.prompt"));
    if (password === null) {
      return;
    }
    if (!isValidPassword(password)) {
      onInvalid("admin.error.password");
      return;
    }
    await updateUser(user.id, { password });
  }

  return (
    <tr>
      <td>{user.username}</td>
      <td>{t(`role.${user.role}`)}</td>
      <td>{formatDate(user.created_at)}</td>
      <td>{user.attempts}</td>
      <td>
        {user.disabled_at === null
          ? t("admin.user.enabled")
          : t("admin.user.disabled")}
      </td>
      <td className="admin-table__actions">
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={() => void handleResetPassword()}
        >
          {t("admin.action.resetPassword")}
        </button>
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={() =>
            void updateUser(user.id, { disabled: user.disabled_at === null })
          }
        >
          {user.disabled_at === null
            ? t("admin.action.disable")
            : t("admin.action.enable")}
        </button>
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={() =>
            void updateUser(user.id, {
              role: user.role === "admin" ? "user" : "admin",
            })
          }
        >
          {user.role === "admin"
            ? t("admin.action.makeUser")
            : t("admin.action.makeAdmin")}
        </button>
      </td>
    </tr>
  );
}

function UsersTable({
  users,
  me,
  onInvalid,
}: {
  users: AdminUser[];
  me: Me;
  onInvalid: (key: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <table className="admin-table" aria-label={t("admin.users")}>
      <thead>
        <tr>
          <th>{t("admin.user.username")}</th>
          <th>{t("admin.user.role")}</th>
          <th>{t("admin.user.created")}</th>
          <th>{t("admin.user.attempts")}</th>
          <th>{t("admin.user.status")}</th>
          <th>{t("admin.user.actions")}</th>
        </tr>
      </thead>
      <tbody>
        {users.map((user) => (
          <UserRow
            key={user.id}
            user={user}
            self={user.id === me.id}
            onInvalid={onInvalid}
          />
        ))}
      </tbody>
    </table>
  );
}

function AttemptRow({ attempt }: { attempt: AdminAttempt }) {
  const { t } = useTranslation();
  const pending = useAdminStore((state) => state.pending);
  const abandonAttempt = useAdminStore((state) => state.abandonAttempt);

  function handleAbandon() {
    if (!window.confirm(t("admin.attempts.abandon.confirm"))) {
      return;
    }
    void abandonAttempt(attempt.id);
  }

  return (
    <tr>
      <td>{attempt.user.username}</td>
      <td>{attempt.lab.title}</td>
      <td>{t(`lab.mode.${attempt.mode}`)}</td>
      <td>{t(`status.${attempt.status}`)}</td>
      <td>{formatElapsed(attempt.elapsed_ms)}</td>
      <td>
        <button
          type="button"
          className="button button--danger"
          disabled={pending === attempt.id}
          onClick={handleAbandon}
        >
          {t("admin.attempts.abandon")}
        </button>
      </td>
    </tr>
  );
}

function AttemptsTable({ attempts }: { attempts: AdminAttempt[] }) {
  const { t } = useTranslation();

  if (attempts.length === 0) {
    return <p className="state">{t("admin.attempts.empty")}</p>;
  }

  return (
    <table className="admin-table" aria-label={t("admin.attempts.heading")}>
      <thead>
        <tr>
          <th>{t("admin.user.username")}</th>
          <th>{t("admin.attempts.lab")}</th>
          <th>{t("lab.modes")}</th>
          <th>{t("result.status")}</th>
          <th>{t("result.elapsed")}</th>
          <th>{t("admin.user.actions")}</th>
        </tr>
      </thead>
      <tbody>
        {attempts.map((attempt) => (
          <AttemptRow key={attempt.id} attempt={attempt} />
        ))}
      </tbody>
    </table>
  );
}

function AdminView({ me }: { me: Me }) {
  const { t } = useTranslation();
  const [invalid, setInvalid] = useState<string | null>(null);
  const users = useAdminStore((state) => state.users);
  const stats = useAdminStore((state) => state.stats);
  const attempts = useAdminStore((state) => state.attempts);
  const actionError = useAdminStore((state) => state.actionError);
  const load = useAdminStore((state) => state.load);

  useEffect(() => {
    void load();
  }, [load]);

  function handleInvalid(key: string) {
    setInvalid(key);
  }

  return (
    <>
      <h2 className="main__heading">{t("admin.heading")}</h2>

      <section className="admin-section">
        <h3 className="admin-section__heading">{t("admin.stats.heading")}</h3>
        {stats.status === "loading" ? (
          <p className="state">{t("admin.stats.loading")}</p>
        ) : null}
        {stats.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("admin.stats.error")}</span>
            <span className="banner__detail">{stats.message}</span>
          </div>
        ) : null}
        {stats.status === "ok" ? <StatsStrip stats={stats.stats} /> : null}
      </section>

      <section className="admin-section">
        <h3 className="admin-section__heading">{t("admin.users")}</h3>

        {invalid !== null ? (
          <div className="banner banner--error">
            <span>{t(invalid)}</span>
          </div>
        ) : null}

        {actionError !== null ? (
          <div className="banner banner--error">
            <span>
              {t(`admin.error.${actionError.code}`, {
                defaultValue: actionError.message,
              })}
            </span>
          </div>
        ) : null}

        {users.status === "loading" ? (
          <p className="state">{t("admin.users.loading")}</p>
        ) : null}
        {users.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("admin.users.error")}</span>
            <span className="banner__detail">{users.message}</span>
          </div>
        ) : null}
        {users.status === "ok" ? (
          <UsersTable users={users.users} me={me} onInvalid={handleInvalid} />
        ) : null}

        <h3 className="admin-section__heading">{t("admin.create.heading")}</h3>
        <CreateUserForm onInvalid={handleInvalid} />
      </section>

      <section className="admin-section">
        <h3 className="admin-section__heading">
          {t("admin.attempts.heading")}
        </h3>
        {attempts.status === "loading" ? (
          <p className="state">{t("admin.attempts.loading")}</p>
        ) : null}
        {attempts.status === "error" ? (
          <div className="banner banner--error">
            <span>{t("admin.attempts.error")}</span>
            <span className="banner__detail">{attempts.message}</span>
          </div>
        ) : null}
        {attempts.status === "ok" ? (
          <AttemptsTable attempts={attempts.attempts} />
        ) : null}
      </section>
    </>
  );
}

export function AdminPage() {
  const { t } = useTranslation();
  const me = useAuthStore((state) => state.me);

  return (
    <div className="page">
      <Header />
      <main className="main">
        {me !== null && me.role === "admin" ? (
          <AdminView me={me} />
        ) : (
          <div className="banner banner--error">
            <span>{t("admin.forbidden")}</span>
          </div>
        )}
      </main>
    </div>
  );
}
