import { labs, summary } from "./fixtures.mjs";

const lab = labs[0];

const ticket = "The server cannot reach the gateway.\nFind out why and fix it.";

const instructions = {
  "link-up":
    "## Bring the link up\n\nOn `host`, look at the interfaces and bring the\ndown one back up:\n\n```sh\nip link\nip link set eth0 up\n```\n",
  "ping-ok":
    "## Reach the gateway\n\nCheck the address and the route, then ping the\ngateway:\n\n```sh\nip addr\nping -c1 10.0.0.1\n```\n",
};

export const solution = "## Fix\n```sh\nip link set eth0 up\n```\n";

const hiddenCheckpoint = {
  id: "route-ok",
  title: "The default route is in place",
};

export function modeOf(id) {
  if (id.endsWith("-tutorial")) {
    return "tutorial";
  }
  if (id.endsWith("-real")) {
    return "real";
  }
  return "guided";
}

const states = new Map();

export function attemptState(id) {
  const known = states.get(id);
  if (known !== undefined) {
    return known;
  }
  const state = {
    id,
    mode: modeOf(id),
    startedAt: Date.now(),
    status: id.endsWith("-retry") ? "provisioning" : "running",
    submitCount: 0,
    checkpoints: lab.checkpoints.map((checkpoint) => ({
      ...checkpoint,
      status: "pending",
      first_passed_at: null,
    })),
    listeners: new Set(),
  };
  states.set(id, state);
  return state;
}

function broadcast(state, message) {
  for (const listener of state.listeners) {
    listener(message);
  }
}

export function attemptPayload(id) {
  const state = attemptState(id);
  const real = state.mode === "real";
  return {
    id,
    lab_id: lab.id,
    mode: state.mode,
    status: state.status,
    error_message: "",
    lab: summary(lab),
    ticket,
    nodes: lab.nodes,
    checkpoints: real ? [] : state.checkpoints,
    checkpoints_hidden: real,
    tutorial_steps:
      state.mode === "tutorial"
        ? lab.checkpoints.map((checkpoint) => ({
            checkpoint: checkpoint.id,
            instruction: instructions[checkpoint.id] ?? "",
          }))
        : null,
    submit_count: state.submitCount,
    elapsed_ms: Date.now() - state.startedAt,
    started_at: new Date(state.startedAt).toISOString(),
    ended_at: null,
    server_time: new Date().toISOString(),
    created_at: new Date(state.startedAt).toISOString(),
  };
}

export function resultPayload(id) {
  const state = attemptState(id);
  const fallbackPassedAt = new Date(state.startedAt).toISOString();
  return {
    attempt_id: id,
    status: state.status === "running" ? "passed" : state.status,
    lab: summary(lab),
    elapsed_ms: Date.now() - state.startedAt,
    command_count: 17,
    checkpoints: [
      {
        ...hiddenCheckpoint,
        status: "pass",
        visible: false,
        first_passed_at: fallbackPassedAt,
      },
      ...state.checkpoints.map((checkpoint) => ({
        ...checkpoint,
        status: "pass",
        visible: true,
        first_passed_at: checkpoint.first_passed_at ?? fallbackPassedAt,
      })),
    ],
    submit_count: state.submitCount,
    solution,
  };
}

export function activeAttempts() {
  return [...states.values()]
    .filter(
      (state) => state.status === "provisioning" || state.status === "running",
    )
    .map((state) => ({
      id: state.id,
      lab: summary(lab),
      mode: state.mode,
      status: state.status,
      elapsed_ms: Date.now() - state.startedAt,
      created_at: new Date(state.startedAt).toISOString(),
      user: state.owner ?? { id: "u-alice", username: "alice" },
    }));
}

export function abandonPayload(id) {
  const state = attemptState(id);
  state.status = "abandoned";
  return attemptPayload(id);
}

export function submit(id) {
  const state = attemptState(id);
  if (state.mode !== "real") {
    return {
      status: 400,
      body: { error: { code: "mode_not_real", message: state.mode } },
    };
  }
  state.submitCount += 1;
  const passed = state.submitCount > 1;
  const now = new Date().toISOString();
  state.checkpoints = state.checkpoints.map((checkpoint, index) => ({
    ...checkpoint,
    status: passed || index === 0 ? "pass" : "fail",
    first_passed_at: passed || index === 0 ? now : null,
  }));
  const body = {
    passed,
    checkpoints: state.checkpoints.map(({ id: cid, title, status }) => ({
      id: cid,
      title,
      status,
    })),
    hidden_failed: passed ? 0 : 1,
    submit_count: state.submitCount,
  };
  if (passed) {
    state.status = "passed";
    broadcast(state, {
      type: "status",
      status: "passed",
      error_message: "",
      elapsed_ms: Date.now() - state.startedAt,
      server_time: now,
    });
  } else {
    broadcast(state, {
      type: "submit",
      passed: false,
      hidden_failed: body.hidden_failed,
      submit_count: state.submitCount,
    });
  }
  return { body };
}

const retrySteps = [
  { step: "networks" },
  { step: "containers" },
  { step: "bootstrap" },
  { step: "setup" },
  { step: "precheck", attempt: 1 },
  { step: "containers" },
  { step: "bootstrap" },
  { step: "setup" },
  { step: "precheck", attempt: 2 },
];

export function serveEvents(connection, id) {
  const state = attemptState(id);
  const send = (message) => connection.send(JSON.stringify(message));
  const status = (value) =>
    send({
      type: "status",
      status: value,
      error_message: "",
      elapsed_ms: Date.now() - state.startedAt,
      server_time: new Date().toISOString(),
    });
  const timers = [];

  state.listeners.add(send);
  status(state.status);

  timers.push(
    setInterval(() => {
      send({
        type: "tick",
        elapsed_ms: Date.now() - state.startedAt,
        server_time: new Date().toISOString(),
      });
    }, 10000),
  );

  if (state.status === "provisioning") {
    retrySteps.forEach((message, index) => {
      timers.push(
        setTimeout(
          () => send({ type: "provisioning", ...message }),
          (index + 1) * 1500,
        ),
      );
    });
    timers.push(
      setTimeout(
        () => {
          state.status = "running";
          state.startedAt = Date.now();
          status("running");
        },
        (retrySteps.length + 1) * 1500,
      ),
    );
  }

  if (state.mode !== "real") {
    const every = state.mode === "tutorial" ? 8000 : 15000;
    state.checkpoints.forEach((checkpoint, index) => {
      timers.push(
        setTimeout(
          () => {
            checkpoint.status = "pass";
            checkpoint.first_passed_at = new Date().toISOString();
            send({
              type: "checkpoint",
              id: checkpoint.id,
              status: "pass",
              first_passed_at: checkpoint.first_passed_at,
            });
          },
          every * (index + 1),
        ),
      );
    });
  }

  connection.on("close", () => {
    state.listeners.delete(send);
    for (const timer of timers) {
      clearInterval(timer);
      clearTimeout(timer);
    }
  });
}
