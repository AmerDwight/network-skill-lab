import { createServer } from "node:http";

import { WebSocketServer } from "ws";

import { docs, health, labs, topics, tracks } from "./fixtures.mjs";

const port = 18090;
const startedAt = Date.now();
const lab = labs[0];
const summary = ({ nodes, checkpoints, topic_title, ...rest }) => rest;
const checkpoints = lab.checkpoints.map((checkpoint) => ({
  ...checkpoint,
  status: "pending",
  first_passed_at: null,
}));
const solution = `## Fix
\`\`\`sh
ip link set eth0 up
\`\`\`
`;

function attempt(id) {
  return {
    id,
    lab_id: lab.id,
    mode: "guided",
    status: "running",
    error_message: "",
    lab: summary(lab),
    ticket: "The server cannot reach the gateway.\nFind out why and fix it.",
    nodes: lab.nodes,
    checkpoints,
    elapsed_ms: Date.now() - startedAt,
    started_at: new Date(startedAt).toISOString(),
    ended_at: null,
    server_time: new Date().toISOString(),
    created_at: new Date(startedAt).toISOString(),
  };
}

function result(id) {
  return {
    attempt_id: id,
    status: "passed",
    lab: summary(lab),
    elapsed_ms: 254000,
    command_count: 17,
    checkpoints: checkpoints.map((checkpoint, index) => ({
      ...checkpoint,
      status: "pass",
      first_passed_at: new Date(startedAt + index * 60000).toISOString(),
    })),
    solution,
  };
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
  ["POST", /^\/api\/attempts$/, () => ({ body: attempt("01JMOCKATTEMPT") })],
  [
    "GET",
    /^\/api\/attempts\/([^/]+)$/,
    (match) => ({ body: attempt(match[1]) }),
  ],
  [
    "GET",
    /^\/api\/attempts\/([^/]+)\/result$/,
    (match) => ({ body: result(match[1]) }),
  ],
  [
    "POST",
    /^\/api\/attempts\/([^/]+)\/abandon$/,
    (match) => ({ body: { ...attempt(match[1]), status: "abandoned" } }),
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

const server = createServer((request, response) => {
  const url = new URL(request.url, "http://localhost");
  void readBody(request).then((body) => {
    for (const [method, pattern, handler] of routes) {
      const match = pattern.exec(url.pathname);
      if (match === null || method !== request.method) {
        continue;
      }
      const reply = handler(match, url, body);
      if (reply.status === 204) {
        response.writeHead(204);
        response.end();
        return;
      }
      response.writeHead(reply.status ?? 200, {
        "Content-Type": "application/json",
      });
      response.end(JSON.stringify(reply.body));
      return;
    }
    response.writeHead(404, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({ error: { code: "not_found", message: url.pathname } }),
    );
  });
});

const sockets = new WebSocketServer({ noServer: true });

server.on("upgrade", (request, socket, head) => {
  const path = new URL(request.url, "http://localhost").pathname;
  sockets.handleUpgrade(request, socket, head, (connection) => {
    if (path.endsWith("/events")) {
      serveEvents(connection);
    } else {
      serveTerminal(connection);
    }
  });
});

function serveEvents(connection) {
  const send = (message) => connection.send(JSON.stringify(message));
  send({
    type: "status",
    status: "running",
    error_message: "",
    elapsed_ms: Date.now() - startedAt,
    server_time: new Date().toISOString(),
  });
  const tick = setInterval(() => {
    send({
      type: "tick",
      elapsed_ms: Date.now() - startedAt,
      server_time: new Date().toISOString(),
    });
  }, 10000);
  const pass = setTimeout(() => {
    checkpoints[0].status = "pass";
    checkpoints[0].first_passed_at = new Date().toISOString();
    send({
      type: "checkpoint",
      id: checkpoints[0].id,
      status: "pass",
      first_passed_at: checkpoints[0].first_passed_at,
    });
  }, 15000);
  connection.on("close", () => {
    clearInterval(tick);
    clearTimeout(pass);
  });
}

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
