import type { FormEvent } from "react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { AdminRunner, CreatedRunner, LabEnvironment } from "../api/types";
import { formatDate } from "../lib/format";
import { useAdminStore } from "../store/admin";

const runnerNamePattern = /^[a-z0-9][a-z0-9_-]{2,31}$/;
const localRunner = "local";
const environments: readonly LabEnvironment[] = ["container", "vm"];
const defaultCapacity = 3;

export function isValidRunnerName(value: string): boolean {
  return runnerNamePattern.test(value) && value !== localRunner;
}

export function runnerSocketUrl(location: Pick<Location, "protocol" | "host">) {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${location.host}/ws/runner`;
}

export function runnerCommand(name: string, url: string): string {
  return `nsl runner --url ${url} --name ${name} --token-file ./${name}.token`;
}

function RunnerStatus({ runner }: { runner: AdminRunner }) {
  const { t } = useTranslation();
  if (runner.revoked_at !== null) {
    return (
      <span className="badge badge--error">{t("admin.runners.revoked")}</span>
    );
  }
  if (runner.connected) {
    return (
      <span className="badge badge--running">
        {t("admin.runners.connected")}
      </span>
    );
  }
  return <span className="badge">{t("admin.runners.offline")}</span>;
}

function RunnerRow({ runner }: { runner: AdminRunner }) {
  const { t } = useTranslation();
  const pending = useAdminStore((state) => state.pending);
  const revokeRunner = useAdminStore((state) => state.revokeRunner);
  const builtin = runner.id === "";

  function handleRevoke() {
    if (
      !window.confirm(t("admin.runners.revoke.confirm", { name: runner.name }))
    ) {
      return;
    }
    void revokeRunner(runner.id);
  }

  return (
    <tr>
      <td>{runner.name}</td>
      <td>
        {runner.capabilities
          .map((env) => t(`env.${env}`, { defaultValue: env }))
          .join(", ")}
      </td>
      <td>{runner.capacity}</td>
      <td>{runner.active}</td>
      <td>
        <RunnerStatus runner={runner} />
      </td>
      <td>
        {builtin
          ? "—"
          : runner.last_seen_at === null
            ? t("admin.runners.never")
            : formatDate(runner.last_seen_at)}
      </td>
      <td>{builtin ? t("admin.runners.builtin") : runner.version || "—"}</td>
      <td>
        {builtin || runner.revoked_at !== null ? null : (
          <button
            type="button"
            className="button button--danger"
            disabled={pending === runner.id}
            onClick={handleRevoke}
          >
            {t("admin.runners.revoke")}
          </button>
        )}
      </td>
    </tr>
  );
}

function RunnersTable({ runners }: { runners: AdminRunner[] }) {
  const { t } = useTranslation();

  if (runners.length === 0) {
    return <p className="state">{t("admin.runners.empty")}</p>;
  }

  return (
    <table className="admin-table" aria-label={t("admin.runners.heading")}>
      <thead>
        <tr>
          <th>{t("admin.runners.name")}</th>
          <th>{t("admin.runners.capabilities")}</th>
          <th>{t("admin.runners.capacity")}</th>
          <th>{t("admin.runners.active")}</th>
          <th>{t("admin.runners.status")}</th>
          <th>{t("admin.runners.lastSeen")}</th>
          <th>{t("admin.runners.version")}</th>
          <th>{t("admin.user.actions")}</th>
        </tr>
      </thead>
      <tbody>
        {runners.map((runner) => (
          <RunnerRow key={runner.id || runner.name} runner={runner} />
        ))}
      </tbody>
    </table>
  );
}

function CreatedToken({ created }: { created: CreatedRunner }) {
  const { t } = useTranslation();
  const dismiss = useAdminStore((state) => state.dismissCreatedRunner);
  const command = runnerCommand(created.name, runnerSocketUrl(window.location));

  return (
    <div className="banner banner--info runner-token" role="status">
      <strong>
        {t("admin.runners.token.heading", { name: created.name })}
      </strong>
      <span>{t("admin.runners.token.once")}</span>
      <pre className="runner-token__value">
        <code>{created.token}</code>
      </pre>
      <span>{t("admin.runners.token.command")}</span>
      <pre className="runner-token__value">
        <code>{command}</code>
      </pre>
      <button type="button" className="button" onClick={dismiss}>
        {t("admin.runners.token.done")}
      </button>
    </div>
  );
}

function CreateRunnerForm({ onInvalid }: { onInvalid: (key: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [capabilities, setCapabilities] = useState<LabEnvironment[]>([
    "container",
  ]);
  const [capacity, setCapacity] = useState(String(defaultCapacity));
  const pending = useAdminStore((state) => state.pending);
  const createRunner = useAdminStore((state) => state.createRunner);

  function toggle(env: LabEnvironment, checked: boolean) {
    setCapabilities((current) =>
      checked
        ? environments.filter((item) => item === env || current.includes(item))
        : current.filter((item) => item !== env),
    );
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    const count = Number(capacity);
    if (!isValidRunnerName(name)) {
      onInvalid("admin.runners.error.name");
      return;
    }
    if (capabilities.length === 0) {
      onInvalid("admin.runners.error.capabilities");
      return;
    }
    if (!Number.isInteger(count) || count < 1) {
      onInvalid("admin.runners.error.capacity");
      return;
    }
    onInvalid("");
    if (await createRunner({ name, capabilities, capacity: count })) {
      setName("");
      setCapabilities(["container"]);
      setCapacity(String(defaultCapacity));
    }
  }

  return (
    <form
      className="admin-form"
      onSubmit={(event) => void handleSubmit(event)}
      aria-label={t("admin.runners.create.heading")}
    >
      <label className="field" htmlFor="runner-name">
        <span className="field__label">{t("admin.runners.create.name")}</span>
        <input
          id="runner-name"
          className="field__input"
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
      </label>

      <fieldset className="field runner-capabilities">
        <legend className="field__label">
          {t("admin.runners.capabilities")}
        </legend>
        {environments.map((env) => (
          <label key={env} className="field--inline">
            <input
              type="checkbox"
              checked={capabilities.includes(env)}
              onChange={(event) => toggle(env, event.target.checked)}
            />
            {t(`env.${env}`)}
          </label>
        ))}
      </fieldset>

      <label className="field" htmlFor="runner-capacity">
        <span className="field__label">
          {t("admin.runners.create.capacity")}
        </span>
        <input
          id="runner-capacity"
          className="field__input"
          type="number"
          min={1}
          value={capacity}
          onChange={(event) => setCapacity(event.target.value)}
        />
      </label>

      <button
        type="submit"
        className="button button--primary"
        disabled={pending === "create-runner"}
      >
        {t("admin.runners.create.submit")}
      </button>
    </form>
  );
}

export function AdminRunners() {
  const { t } = useTranslation();
  const [invalid, setInvalid] = useState("");
  const runners = useAdminStore((state) => state.runners);
  const created = useAdminStore((state) => state.createdRunner);
  const runnerError = useAdminStore((state) => state.runnerError);

  return (
    <section className="admin-section">
      <h3 className="admin-section__heading">{t("admin.runners.heading")}</h3>

      {invalid !== "" ? (
        <div className="banner banner--error">
          <span>{t(invalid)}</span>
        </div>
      ) : null}
      {runnerError !== null ? (
        <div className="banner banner--error">
          <span>
            {t(`admin.runners.error.${runnerError.code}`, {
              defaultValue: runnerError.message,
            })}
          </span>
        </div>
      ) : null}
      {created !== null ? <CreatedToken created={created} /> : null}

      {runners.status === "loading" ? (
        <p className="state">{t("admin.runners.loading")}</p>
      ) : null}
      {runners.status === "error" ? (
        <div className="banner banner--error">
          <span>{t("admin.runners.error")}</span>
          <span className="banner__detail">{runners.message}</span>
        </div>
      ) : null}
      {runners.status === "ok" ? (
        <RunnersTable runners={runners.runners} />
      ) : null}

      <h3 className="admin-section__heading">
        {t("admin.runners.create.heading")}
      </h3>
      <CreateRunnerForm onInvalid={setInvalid} />
    </section>
  );
}
