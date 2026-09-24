import { currentLanguage } from "../i18n";
import { encodePathSegments } from "../lib/paths";

import { notifyUnauthorized } from "./session";
import type {
  AdminAttempt,
  AdminStats,
  AdminUser,
  Attempt,
  CommandEntry,
  CommandsQuery,
  CreateAttemptRequest,
  CreateUserRequest,
  Doc,
  DocSummary,
  Health,
  HistoryItem,
  HistoryQuery,
  LabDetail,
  LabMode,
  LabSummary,
  LoginRequest,
  Me,
  ProgressRequest,
  RecordingInfo,
  Result,
  RunnerBusy,
  SubmitResult,
  TopicNode,
  Track,
  TrackSummary,
  UpdateMeRequest,
  UpdateUserRequest,
} from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: Readonly<Record<string, unknown>>;

  constructor(
    status: number,
    code: string,
    message: string,
    details: Readonly<Record<string, unknown>> = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export const loginPath = "/api/auth/login";

export function runnerBusyOf(error: unknown): RunnerBusy | null {
  if (!(error instanceof ApiError) || error.code !== "runner_busy") {
    return null;
  }
  const { sandboxes_active: active, sandboxes_max: max } = error.details;
  if (typeof active !== "number" || typeof max !== "number") {
    return null;
  }
  return { sandboxes_active: active, sandboxes_max: max };
}

function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined) {
      search.set(key, String(value));
    }
  }
  const text = search.toString();
  return text === "" ? "" : `?${text}`;
}

function withLanguage(path: string): string {
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}lang=${encodeURIComponent(currentLanguage())}`;
}

function parseError(status: number, body: unknown): ApiError {
  const fallback = new ApiError(
    status,
    "unknown",
    `request failed with status ${status}`,
  );
  if (typeof body !== "object" || body === null) {
    return fallback;
  }
  const { error, ...details } = body as {
    error?: { code?: unknown; message?: unknown };
  };
  if (
    error === undefined ||
    typeof error.code !== "string" ||
    typeof error.message !== "string"
  ) {
    return fallback;
  }
  return new ApiError(status, error.code, error.message, details);
}

async function send(path: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(withLanguage(path), init);
  if (response.ok) {
    return response;
  }
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  const error = parseError(response.status, body);
  if (response.status === 401 && path !== loginPath) {
    notifyUnauthorized();
  }
  throw error;
}

function jsonRequest(method: string, body?: unknown): RequestInit {
  if (body === undefined) {
    return { method };
  }
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

async function json<T>(response: Response): Promise<T> {
  return (await response.json()) as T;
}

export async function getHealth(): Promise<Health> {
  return json<Health>(await send("/api/health"));
}

export async function listTopics(): Promise<TopicNode[]> {
  return json<TopicNode[]>(await send("/api/topics"));
}

export async function listLabs(topic?: string): Promise<LabSummary[]> {
  const query =
    topic === undefined ? "" : `?topic=${encodeURIComponent(topic)}`;
  return json<LabSummary[]>(await send(`/api/labs${query}`));
}

export async function getLab(id: string): Promise<LabDetail> {
  return json<LabDetail>(await send(`/api/labs/${encodeURIComponent(id)}`));
}

export async function listDocs(): Promise<DocSummary[]> {
  return json<DocSummary[]>(await send("/api/docs"));
}

export async function getDoc(id: string): Promise<Doc> {
  return json<Doc>(await send(`/api/docs/${encodePathSegments(id)}`));
}

export async function markDocRead(id: string): Promise<void> {
  const body: ProgressRequest = { kind: "doc", ref: id };
  await send("/api/progress", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function listTracks(): Promise<TrackSummary[]> {
  return json<TrackSummary[]>(await send("/api/tracks"));
}

export async function getTrack(id: string): Promise<Track> {
  return json<Track>(await send(`/api/tracks/${encodeURIComponent(id)}`));
}

export async function createAttempt(
  labId: string,
  mode: LabMode = "guided",
): Promise<Attempt> {
  const body: CreateAttemptRequest = { lab_id: labId, mode };
  const response = await send("/api/attempts", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return json<Attempt>(response);
}

export async function getCurrentAttempt(): Promise<Attempt | null> {
  const response = await send("/api/attempts/current");
  if (response.status === 204) {
    return null;
  }
  return json<Attempt>(response);
}

export async function getAttempt(id: string): Promise<Attempt> {
  return json<Attempt>(await send(`/api/attempts/${encodeURIComponent(id)}`));
}

export async function abandonAttempt(id: string): Promise<Attempt> {
  const response = await send(
    `/api/attempts/${encodeURIComponent(id)}/abandon`,
    { method: "POST" },
  );
  return json<Attempt>(response);
}

export async function submitAttempt(id: string): Promise<SubmitResult> {
  const response = await send(
    `/api/attempts/${encodeURIComponent(id)}/submit`,
    {
      method: "POST",
    },
  );
  return json<SubmitResult>(response);
}

export async function getResult(id: string): Promise<Result> {
  const response = await send(`/api/attempts/${encodeURIComponent(id)}/result`);
  return json<Result>(response);
}

export async function login(username: string, password: string): Promise<Me> {
  const body: LoginRequest = { username, password };
  return json<Me>(await send(loginPath, jsonRequest("POST", body)));
}

export async function logout(): Promise<void> {
  await send("/api/auth/logout", jsonRequest("POST"));
}

export async function getMe(): Promise<Me> {
  return json<Me>(await send("/api/auth/me"));
}

export async function updateMe(patch: UpdateMeRequest): Promise<Me> {
  return json<Me>(await send("/api/auth/me", jsonRequest("PATCH", patch)));
}

export async function listUsers(): Promise<AdminUser[]> {
  return json<AdminUser[]>(await send("/api/admin/users"));
}

export async function createUser(input: CreateUserRequest): Promise<AdminUser> {
  return json<AdminUser>(
    await send("/api/admin/users", jsonRequest("POST", input)),
  );
}

export async function updateUser(
  id: string,
  patch: UpdateUserRequest,
): Promise<AdminUser> {
  return json<AdminUser>(
    await send(
      `/api/admin/users/${encodeURIComponent(id)}`,
      jsonRequest("PATCH", patch),
    ),
  );
}

export async function listAdminAttempts(): Promise<AdminAttempt[]> {
  return json<AdminAttempt[]>(await send("/api/admin/attempts"));
}

export async function adminAbandon(attemptId: string): Promise<Attempt> {
  return json<Attempt>(
    await send(
      `/api/admin/attempts/${encodeURIComponent(attemptId)}/abandon`,
      jsonRequest("POST"),
    ),
  );
}

export async function getAdminStats(): Promise<AdminStats> {
  return json<AdminStats>(await send("/api/admin/stats"));
}

export async function adminDeleteAttempt(attemptId: string): Promise<void> {
  await send(`/api/admin/attempts/${encodeURIComponent(attemptId)}`, {
    method: "DELETE",
  });
}

export async function listHistory(
  params: HistoryQuery = {},
): Promise<HistoryItem[]> {
  return json<HistoryItem[]>(await send(`/api/history${query(params)}`));
}

export async function getCommands(
  attemptId: string,
  params: CommandsQuery = {},
): Promise<CommandEntry[]> {
  return json<CommandEntry[]>(
    await send(
      `/api/attempts/${encodeURIComponent(attemptId)}/commands${query(params)}`,
    ),
  );
}

export async function listRecordings(
  attemptId: string,
): Promise<RecordingInfo[]> {
  return json<RecordingInfo[]>(
    await send(`/api/attempts/${encodeURIComponent(attemptId)}/recordings`),
  );
}

export function castUrl(attemptId: string, recordingId: string): string {
  const attempt = encodeURIComponent(attemptId);
  const recording = encodeURIComponent(recordingId);
  return `/api/attempts/${attempt}/recordings/${recording}/cast`;
}
