import { createServer } from "node:http";

import { WebSocketServer } from "ws";

const port = 18090;
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
  {
    id: "link-up",
    title: "The link is up",
    status: "pending",
    first_passed_at: null,
  },
  {
    id: "ping-ok",
    title: "The gateway answers",
    status: "pending",
    first_passed_at: null,
  },
];
const startedAt = Date.now();

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

const server = createServer((request, response) => {
  const path = new URL(request.url, "http://localhost").pathname;
  const match = /^\/api\/attempts\/([^/]+)$/.exec(path);
  if (match) {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify(attempt(match[1])));
    return;
  }
  if (/^\/api\/attempts\/[^/]+\/abandon$/.test(path)) {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ ...attempt("mock"), status: "abandoned" }));
    return;
  }
  response.writeHead(404, { "Content-Type": "application/json" });
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
