import { createServer } from "node:http";

import { WebSocketServer } from "ws";

import {
  abandonPayload,
  activeAttempts,
  attemptPayload,
  attemptState,
  resultPayload,
  serveEvents,
  submit,
} from "./attempts.mjs";
import {
  createUser,
  forbidden,
  listUsers,
  login,
  logout,
  me,
  patchMe,
  patchUser,
  sessionUser,
  unauthorized,
} from "./auth.mjs";
import { docs, health, labs, topics, tracks } from "./fixtures.mjs";

const port = 18090;

const sandboxesMax = 3;

const publicPaths = new Set(["/api/auth/login"]);

function sandboxStats() {
  return {
    sandboxes_active: activeAttempts().length,
    sandboxes_max: sandboxesMax,
  };
}

function startAttempt(url, body, user) {
  const stats = sandboxStats();
  const busy =
    url.searchParams.get("busy") === "1" ||
    process.env.NSL_MOCK_BUSY === "1" ||
    stats.sandboxes_active >= stats.sandboxes_max;
  if (busy) {
    return {
      status: 429,
      body: {
        error: { code: "runner_busy", message: "every sandbox is in use" },
        ...stats,
      },
    };
  }
  const id = `01JMOCKATTEMPT-${body?.mode ?? "guided"}`;
  const state = attemptState(id);
  state.owner = { id: user.id, username: user.username };
  return { body: attemptPayload(id) };
}

const inTopic = (topic, selected) =>
  selected === null || topic === selected || topic.startsWith(`${selected}/`);

const localized = (value, lang) => value[lang] ?? value.en;

function docPayload(doc, lang) {
  return {
    id: doc.id,
    topic: doc.topic,
    title: localized(doc.title, lang),
    body: localized(doc.body, lang),
    completed: doc.completed,
  };
}

function markRead(body) {
  if (body.kind !== "doc") {
    return {
      status: 400,
      body: { error: { code: "invalid_kind", message: body.kind } },
    };
  }
  const doc = docs.find((candidate) => candidate.id === body.ref);
  if (doc === undefined) {
    return {
      status: 404,
      body: { error: { code: "not_found", message: body.ref } },
    };
  }
  doc.completed = true;
  for (const track of tracks) {
    for (const step of track.steps) {
      if (step.kind === "doc" && step.ref === doc.id) {
        step.completed = true;
      }
    }
  }
  return { status: 204 };
}

function found(value, id) {
  if (value === undefined) {
    return { status: 404, body: { error: { code: "not_found", message: id } } };
  }
  return { body: value };
}

const routes = [
  ["POST", /^\/api\/auth\/login$/, (match, url, body) => login(body)],
  [
    "POST",
    /^\/api\/auth\/logout$/,
    (match, url, body, user, request) => logout(request),
  ],
  ["GET", /^\/api\/auth\/me$/, (match, url, body, user) => me(user)],
  [
    "PATCH",
    /^\/api\/auth\/me$/,
    (match, url, body, user) => patchMe(user, body),
  ],
  ["GET", /^\/api\/admin\/users$/, () => listUsers()],
  ["POST", /^\/api\/admin\/users$/, (match, url, body) => createUser(body)],
  [
    "PATCH",
    /^\/api\/admin\/users\/([^/]+)$/,
    (match, url, body, user) =>
      patchUser(user, decodeURIComponent(match[1]), body),
  ],
  ["GET", /^\/api\/admin\/attempts$/, () => ({ body: activeAttempts() })],
  [
    "POST",
    /^\/api\/admin\/attempts\/([^/]+)\/abandon$/,
    (match) => ({ body: abandonPayload(decodeURIComponent(match[1])) }),
  ],
  [
    "GET",
    /^\/api\/admin\/stats$/,
    () => ({
      body: { ...sandboxStats(), recordings_bytes: 734003200, attempts: 12 },
    }),
  ],
  ["GET", /^\/api\/health$/, () => ({ body: health })],
  ["GET", /^\/api\/topics$/, () => ({ body: topics })],
  [
    "GET",
    /^\/api\/labs$/,
    (match, url) => ({
      body: labs
        .filter((item) => inTopic(item.topic, url.searchParams.get("topic")))
        .map(summary),
    }),
  ],
  [
    "GET",
    /^\/api\/labs\/([^/]+)$/,
    (match) => {
      const id = decodeURIComponent(match[1]);
      return found(
        labs.find((item) => item.id === id),
        id,
      );
    },
  ],
  [
    "GET",
    /^\/api\/docs$/,
    (match, url) => ({
      body: docs.map((doc) => ({
        id: doc.id,
        topic: doc.topic,
        title: localized(doc.title, url.searchParams.get("lang") ?? "en"),
      })),
    }),
  ],
  [
    "GET",
    /^\/api\/docs\/(.+)$/,
    (match, url) => {
      const id = decodeURIComponent(match[1]);
      const doc = docs.find((candidate) => candidate.id === id);
      return doc === undefined
        ? found(undefined, id)
        : { body: docPayload(doc, url.searchParams.get("lang") ?? "en") };
    },
  ],
  [
    "GET",
    /^\/api\/tracks$/,
    () => ({
      body: tracks.map((track) => ({
        id: track.id,
        title: track.title,
        steps: track.steps.length,
        completed: track.steps.filter((step) => step.completed).length,
      })),
    }),
  ],
  [
    "GET",
    /^\/api\/tracks\/([^/]+)$/,
    (match) => {
      const id = decodeURIComponent(match[1]);
      return found(
        tracks.find((track) => track.id === id),
        id,
      );
    },
  ],
  ["POST", /^\/api\/progress$/, (match, url, body) => markRead(body)],
  ["GET", /^\/api\/attempts\/current$/, () => ({ status: 204 })],
  [
    "POST",
    /^\/api\/attempts$/,
    (match, url, body, user) => startAttempt(url, body, user),
  ],
  [
    "GET",
    /^\/api\/attempts\/([^/]+)$/,
    (match) => ({ body: attemptPayload(decodeURIComponent(match[1])) }),
  ],
  [
    "GET",
    /^\/api\/attempts\/([^/]+)\/result$/,
    (match) => ({ body: resultPayload(decodeURIComponent(match[1])) }),
  ],
  [
    "POST",
    /^\/api\/attempts\/([^/]+)\/submit$/,
    (match) => submit(decodeURIComponent(match[1])),
  ],
  [
    "POST",
    /^\/api\/attempts\/([^/]+)\/abandon$/,
    (match) => ({ body: abandonPayload(decodeURIComponent(match[1])) }),
  ],
];

async function readBody(request) {
  const chunks = [];
  for await (const chunk of request) {
    chunks.push(chunk);
  }
  if (chunks.length === 0) {
    return null;
  }
  return JSON.parse(Buffer.concat(chunks).toString());
}

function dispatch(request, url, body) {
  const user = sessionUser(request);
  if (user === null && !publicPaths.has(url.pathname)) {
    return unauthorized();
  }
  if (url.pathname.startsWith("/api/admin/") && user.role !== "admin") {
    return forbidden();
  }
  for (const [method, pattern, handler] of routes) {
    const match = pattern.exec(url.pathname);
    if (match === null || method !== request.method) {
      continue;
    }
    return handler(match, url, body, user, request);
  }
  return {
    status: 404,
    body: { error: { code: "not_found", message: url.pathname } },
  };
}

function reply(response, result) {
  const headers = { ...(result.headers ?? {}) };
  if (result.status === 204) {
    response.writeHead(204, headers);
    response.end();
    return;
  }
  response.writeHead(result.status ?? 200, {
    ...headers,
    "Content-Type": "application/json",
  });
  response.end(JSON.stringify(result.body));
}

const server = createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  void readBody(request).then((body) => {
    reply(response, dispatch(request, url, body));
  });
});

const sockets = new WebSocketServer({ noServer: true });

const eventsPath = /^\/ws\/attempts\/([^/]+)\/events$/;

function refuseUpgrade(socket) {
  const payload = JSON.stringify({
    error: { code: "unauthorized", message: "sign in first" },
  });
  socket.end(
    [
      "HTTP/1.1 401 Unauthorized",
      "Content-Type: application/json",
      `Content-Length: ${Buffer.byteLength(payload)}`,
      "Connection: close",
      "",
      payload,
    ].join("\r\n"),
  );
}

server.on("upgrade", (request, socket, head) => {
  if (sessionUser(request) === null) {
    refuseUpgrade(socket);
    return;
  }
  const path = new URL(request.url, "http://localhost").pathname;
  const match = eventsPath.exec(path);
  sockets.handleUpgrade(request, socket, head, (connection) => {
    if (match === null) {
      serveTerminal(connection);
    } else {
      serveEvents(connection, decodeURIComponent(match[1]));
    }
  });
});

function serveTerminal(connection) {
  connection.send(Buffer.from("$ "));
  connection.on("message", (data, isBinary) => {
    if (!isBinary) {
      return;
    }
    const text = data.toString();
    connection.send(Buffer.from(text.includes("\r") ? `${text}\n$ ` : text));
  });
}

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`mock api listening on http://127.0.0.1:${port}\n`);
});
