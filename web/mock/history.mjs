import { labs, summary } from "./fixtures.mjs";

const owners = [
  { id: "u-alice", username: "alice" },
  { id: "u-admin", username: "admin" },
];

const statuses = ["passed", "abandoned", "expired", "error"];

const base = Date.parse("2026-09-24T09:00:00Z");

function makeItem(index) {
  const lab = labs[index % labs.length];
  const owner = owners[index % owners.length];
  const running = index === 0;
  const createdAt = base - index * 3600 * 1000;
  const elapsed = 120000 + (index % 7) * 45000;
  return {
    id: `01JHIST${String(index).padStart(4, "0")}`,
    lab: summary(lab),
    mode: lab.modes[index % lab.modes.length],
    status: running ? "running" : statuses[index % statuses.length],
    elapsed_ms: elapsed,
    submit_count: index % 3,
    command_count: 12 + index,
    created_at: new Date(createdAt).toISOString(),
    ended_at: running ? null : new Date(createdAt + elapsed).toISOString(),
    user: owner,
  };
}

const items = Array.from({ length: 60 }, (unused, index) => makeItem(index));

const commandLines = [
  "ip link",
  "ip addr show eth0",
  "ip route",
  "ping -c1 10.0.0.1",
  "cat /etc/resolv.conf",
  "ss -tulpn",
  "journalctl -u systemd-networkd -n 20",
  "ip link set eth0 up",
];

function commandsOf(attemptId) {
  return Array.from({ length: 130 }, (unused, index) => ({
    id: `${attemptId}-cmd-${String(index).padStart(4, "0")}`,
    node: index % 3 === 0 ? "gw" : "host",
    ts: new Date(base + index * 9000).toISOString(),
    user: "root",
    cwd: index % 4 === 0 ? "/etc" : "/root",
    command: commandLines[index % commandLines.length],
    exit_code: index % 11 === 0 ? 1 : 0,
  }));
}

function recordingsOf(attemptId) {
  return [
    {
      id: `${attemptId}-rec-host-1`,
      node: "host",
      tab: "1",
      started_at: new Date(base).toISOString(),
      ended_at: new Date(base + 300000).toISOString(),
      bytes: 48213,
    },
    {
      id: `${attemptId}-rec-gw-1`,
      node: "gw",
      tab: "1",
      started_at: new Date(base + 60000).toISOString(),
      ended_at: new Date(base + 240000).toISOString(),
      bytes: 12044,
    },
  ];
}

const castHeader = {
  version: 2,
  width: 80,
  height: 24,
  timestamp: Math.floor(base / 1000),
  env: { TERM: "xterm-256color", SHELL: "/bin/bash" },
};

const castEvents = [
  [0.1, "o", "\u001b[32mroot@host\u001b[0m:~# "],
  [0.6, "i", "ip link\r"],
  [0.7, "o", "ip link\r\n"],
  [0.9, "o", "1: lo: <LOOPBACK,UP> mtu 65536\r\n"],
  [1.1, "o", "2: eth0: <BROADCAST,MULTICAST> mtu 1500 state DOWN\r\n"],
  [1.4, "o", "\u001b[32mroot@host\u001b[0m:~# "],
  [2.0, "r", "120x30"],
  [2.3, "o", "ip link set eth0 up\r\n"],
  [2.9, "o", "\u001b[32mroot@host\u001b[0m:~# "],
];

export function castOf() {
  return [castHeader, ...castEvents]
    .map((entry) => `${JSON.stringify(entry)}\n`)
    .join("");
}

export function listHistory(user, url) {
  const requested = url.searchParams.get("user_id");
  if (requested !== null && user.role !== "admin") {
    return {
      status: 403,
      body: { error: { code: "forbidden", message: "admins only" } },
    };
  }
  const ownerId = requested ?? user.id;
  const before = url.searchParams.get("before");
  const limit = Number(url.searchParams.get("limit") ?? "20");
  const page = items
    .filter((item) => item.user.id === ownerId)
    .filter((item) => before === null || item.created_at < before)
    .slice(0, limit);
  return { body: page };
}

export function listCommands(attemptId, url) {
  const after = url.searchParams.get("after");
  const limit = Number(url.searchParams.get("limit") ?? "500");
  const all = commandsOf(attemptId);
  const start = after === null ? 0 : all.findIndex((c) => c.id === after) + 1;
  return { body: all.slice(start, start + limit) };
}

export function listRecordings(attemptId) {
  return { body: recordingsOf(attemptId) };
}

export function serveCast() {
  return { text: castOf(), contentType: "text/plain; charset=utf-8" };
}

export function deleteAttempt(attemptId) {
  const index = items.findIndex((item) => item.id === attemptId);
  if (index === -1) {
    return {
      status: 404,
      body: { error: { code: "not_found", message: attemptId } },
    };
  }
  items.splice(index, 1);
  return { status: 204 };
}
