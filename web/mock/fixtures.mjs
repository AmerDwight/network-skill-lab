export const health = {
  ok: true,
  docker: true,
  image: true,
  image_name: "nsl/node",
  instance: "mock",
  mem_available_mb: 4096,
  error: "",
};

export const topics = [
  {
    id: "net",
    title: "Networking",
    labs: 2,
    docs: 2,
    children: [
      { id: "net/ip", title: "IP and links", labs: 1, docs: 1, children: [] },
      { id: "net/dns", title: "DNS", labs: 1, docs: 1, children: [] },
    ],
  },
  {
    id: "k3s",
    title: "Kubernetes",
    labs: 1,
    docs: 0,
    children: [
      { id: "k3s/pods", title: "Pods", labs: 1, docs: 0, children: [] },
    ],
  },
];

const ipGuide = { id: "net/ip/guide", title: "IP troubleshooting guide" };
const dnsGuide = { id: "net/dns/guide", title: "DNS troubleshooting guide" };

export const labs = [
  {
    id: "net-ip-01-link-down",
    title: "Server lost connectivity",
    topic: "net/ip",
    topic_title: "IP and links",
    level: 2,
    modes: ["tutorial", "guided", "real"],
    estimated_minutes: 10,
    related_docs: [ipGuide],
    has_hidden_checkpoints: false,
    nodes: [
      { name: "host", role: "linux" },
      { name: "gw", role: "router" },
    ],
    checkpoints: [
      { id: "link-up", title: "The link is up" },
      { id: "ping-ok", title: "The gateway answers" },
    ],
  },
  {
    id: "net-dns-01-resolved",
    title: "Names no longer resolve",
    topic: "net/dns",
    topic_title: "DNS",
    level: 2,
    modes: ["guided", "real"],
    estimated_minutes: 15,
    related_docs: [dnsGuide],
    has_hidden_checkpoints: true,
    nodes: [{ name: "host", role: "linux" }],
    checkpoints: [{ id: "resolve-ok", title: "The host resolves names" }],
  },
  {
    id: "k3s-pod-01-crashloop",
    title: "A pod keeps restarting",
    topic: "k3s/pods",
    topic_title: "Pods",
    level: 2,
    modes: ["tutorial", "guided"],
    estimated_minutes: 20,
    related_docs: [],
    has_hidden_checkpoints: false,
    nodes: [{ name: "server", role: "k3s-server" }],
    checkpoints: [{ id: "pod-ready", title: "The pod is ready" }],
  },
];

export const docs = [
  {
    id: ipGuide.id,
    topic: "net/ip",
    completed: false,
    title: { "zh-TW": "IP 排查指南", en: ipGuide.title },
    body: {
      "zh-TW":
        "# IP 排查指南\n\n1. `ip link` 看介面狀態\n2. `ip addr` 看位址\n",
      en: "# IP troubleshooting\n\n1. `ip link` for the interface\n2. `ip addr` for addresses\n",
    },
  },
  {
    id: dnsGuide.id,
    topic: "net/dns",
    completed: false,
    title: { "zh-TW": "DNS 排查指南", en: dnsGuide.title },
    body: {
      "zh-TW":
        "# DNS 排查指南\n\n1. `resolvectl status`\n2. `dig example.com`\n",
      en: "# DNS troubleshooting\n\n1. `resolvectl status`\n2. `dig example.com`\n",
    },
  },
];

export const tracks = [
  {
    id: "network-basics",
    title: "Network basics",
    steps: [
      {
        kind: "doc",
        ref: ipGuide.id,
        title: ipGuide.title,
        mode: null,
        completed: true,
      },
      {
        kind: "lab",
        ref: "net-ip-01-link-down",
        title: "Server lost connectivity",
        mode: "tutorial",
        completed: true,
      },
      {
        kind: "lab",
        ref: "net-ip-01-link-down",
        title: "Server lost connectivity",
        mode: "guided",
        completed: false,
      },
      {
        kind: "doc",
        ref: dnsGuide.id,
        title: dnsGuide.title,
        mode: null,
        completed: false,
      },
    ],
  },
];
