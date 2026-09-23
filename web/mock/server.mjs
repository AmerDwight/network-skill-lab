import { createServer } from "node:http";

import { WebSocketServer } from "ws";

const port = 18090;
const startedAt = Date.now();
const lab = {
  id: "net-ip-01-link-down",
  title: "Server lost connectivity",
  topic: "net/ip",
  level: 2,
  modes: ["guided"],
  estimated_minutes: 10,
};
const nodes = [
  { name: "host", role: "linux" },
  { name: "gw", role: "router" },
];
const checkpoints = [
  ["link-up", "The link is up"],
  ["ping-ok", "The gateway answers"],
].map(([id, title]) => ({
  id,
  title,
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
    lab,
    ticket: "The server cannot reach the gateway.\nFind out why and fix it.",
    nodes,
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
    lab,
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

const abandoned = (id) => ({ ...attempt(id), status: "abandoned" });

const routes = [
  [/^\/api\/attempts\/([^/]+)$/, attempt],
  [/^\/api\/attempts\/([^/]+)\/result$/, result],
  [/^\/api\/attempts\/([^/]+)\/abandon$/, abandoned],
];

const server = createServer((request, response) => {
  const path = new URL(request.url, "http://localhost").pathname;
  response.setHeader("Content-Type", "application/json");
  for (const [pattern, body] of routes) {
    const match = pattern.exec(path);
    if (match) {
      response.end(JSON.stringify(body(match[1])));
      return;
    }
  }
  response.writeHead(404);
  response.end(JSON.stringify({ error: { code: "not_found", message: path } }));
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
