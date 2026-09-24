import { randomUUID } from "node:crypto";

export const sessionCookie = "nsl_session";

const seed = [
  {
    id: "u-admin",
    username: "admin",
    password: "admin123",
    role: "admin",
    locale: "zh",
    created_at: "2026-09-01T08:00:00Z",
    disabled_at: null,
    attempts: 4,
  },
  {
    id: "u-alice",
    username: "alice",
    password: "alice123",
    role: "user",
    locale: "en",
    created_at: "2026-09-12T09:30:00Z",
    disabled_at: null,
    attempts: 1,
  },
];

const users = process.env.NSL_MOCK_NO_USERS === "1" ? [] : [...seed];

const sessions = new Map();

const mePayload = (user) => ({
  id: user.id,
  username: user.username,
  role: user.role,
  locale: user.locale,
  created_at: user.created_at,
});

const userPayload = (user) => ({
  ...mePayload(user),
  disabled_at: user.disabled_at,
  attempts: user.attempts,
});

const fail = (status, code, message) => ({
  status,
  body: { error: { code, message } },
});

function cookiesOf(request) {
  const jar = new Map();
  for (const part of (request.headers.cookie ?? "").split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name !== "") {
      jar.set(name, rest.join("="));
    }
  }
  return jar;
}

export function sessionUser(request) {
  const id = cookiesOf(request).get(sessionCookie);
  if (id === undefined) {
    return null;
  }
  const userId = sessions.get(id);
  const user = users.find((candidate) => candidate.id === userId);
  return user === undefined || user.disabled_at !== null ? null : user;
}

export function unauthorized() {
  return users.length === 0
    ? fail(401, "no_users", "no account exists yet")
    : fail(401, "unauthorized", "sign in first");
}

export function forbidden() {
  return fail(403, "forbidden", "admins only");
}

export function login(body) {
  const username = body?.username ?? "";
  const password = body?.password ?? "";
  const user = users.find((candidate) => candidate.username === username);
  if (user === undefined || user.password !== password) {
    return fail(401, "invalid_credentials", "wrong username or password");
  }
  if (user.disabled_at !== null) {
    return fail(403, "user_disabled", "this account is disabled");
  }
  const id = randomUUID();
  sessions.set(id, user.id);
  return {
    body: mePayload(user),
    headers: {
      "Set-Cookie": `${sessionCookie}=${id}; Path=/; HttpOnly; SameSite=Lax`,
    },
  };
}

export function logout(request) {
  const id = cookiesOf(request).get(sessionCookie);
  if (id !== undefined) {
    sessions.delete(id);
  }
  return {
    status: 204,
    headers: {
      "Set-Cookie": `${sessionCookie}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`,
    },
  };
}

export function me(user) {
  return { body: mePayload(user) };
}

export function patchMe(user, body) {
  if (body?.locale === "zh" || body?.locale === "en") {
    user.locale = body.locale;
  }
  return { body: mePayload(user) };
}

export function listUsers() {
  return { body: users.map(userPayload) };
}

export function createUser(body) {
  const username = body?.username ?? "";
  if (users.some((candidate) => candidate.username === username)) {
    return fail(409, "username_taken", username);
  }
  const user = {
    id: `u-${randomUUID().slice(0, 8)}`,
    username,
    password: body?.password ?? "",
    role: body?.role === "admin" ? "admin" : "user",
    locale: "en",
    created_at: new Date().toISOString(),
    disabled_at: null,
    attempts: 0,
  };
  users.push(user);
  return { status: 201, body: userPayload(user) };
}

export function patchUser(actor, id, body) {
  const user = users.find((candidate) => candidate.id === id);
  if (user === undefined) {
    return fail(404, "not_found", id);
  }
  if (user.id === actor.id) {
    return fail(400, "cannot_modify_self", "an admin cannot change itself");
  }
  if (body?.role === "admin" || body?.role === "user") {
    user.role = body.role;
  }
  if (typeof body?.disabled === "boolean") {
    user.disabled_at = body.disabled ? new Date().toISOString() : null;
  }
  if (typeof body?.password === "string" && body.password !== "") {
    user.password = body.password;
  }
  return { body: userPayload(user) };
}
