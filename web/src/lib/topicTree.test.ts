import { describe, expect, it } from "vitest";

import type { TopicNode } from "../api/types";

import {
  ancestorIds,
  expandedTopics,
  toggleTopic,
  topicPath,
  totalLabs,
} from "./topicTree";

const topics: TopicNode[] = [
  {
    id: "net",
    title: "Networking",
    labs: 3,
    docs: 2,
    children: [
      { id: "net/ip", title: "IP", labs: 2, docs: 1, children: [] },
      { id: "net/dns", title: "DNS", labs: 1, docs: 1, children: [] },
    ],
  },
  {
    id: "k3s",
    title: "Kubernetes",
    labs: 2,
    docs: 1,
    children: [
      { id: "k3s/pods", title: "Pods", labs: 2, docs: 1, children: [] },
    ],
  },
];

describe("totalLabs", () => {
  it("sums the roots because their counts already include the children", () => {
    expect(totalLabs(topics)).toBe(5);
  });

  it("is zero without topics", () => {
    expect(totalLabs([])).toBe(0);
  });
});

describe("topicPath", () => {
  it("returns the chain down to a child", () => {
    expect(topicPath(topics, "net/dns").map((topic) => topic.id)).toEqual([
      "net",
      "net/dns",
    ]);
  });

  it("returns the root alone", () => {
    expect(topicPath(topics, "k3s").map((topic) => topic.id)).toEqual(["k3s"]);
  });

  it("is empty for an unknown or missing selection", () => {
    expect(topicPath(topics, "net/mpls")).toEqual([]);
    expect(topicPath(topics, null)).toEqual([]);
  });
});

describe("ancestorIds", () => {
  it("leaves out the selected topic itself", () => {
    expect(ancestorIds(topics, "k3s/pods")).toEqual(["k3s"]);
    expect(ancestorIds(topics, "k3s")).toEqual([]);
    expect(ancestorIds(topics, null)).toEqual([]);
  });
});

describe("expandedTopics", () => {
  it("opens the ancestors of the selected topic by default", () => {
    expect([...expandedTopics(topics, "net/dns", new Map())]).toEqual(["net"]);
    expect([...expandedTopics(topics, null, new Map())]).toEqual([]);
  });

  it("lets an override open or close any topic", () => {
    expect([
      ...expandedTopics(topics, null, toggleTopic(new Map(), "k3s", true)),
    ]).toEqual(["k3s"]);
    expect([
      ...expandedTopics(
        topics,
        "net/dns",
        toggleTopic(new Map(), "net", false),
      ),
    ]).toEqual([]);
  });
});

describe("toggleTopic", () => {
  it("does not change the overrides it was given", () => {
    const overrides = toggleTopic(new Map(), "net", true);

    expect([...toggleTopic(overrides, "k3s", true).keys()]).toEqual([
      "net",
      "k3s",
    ]);
    expect([...overrides.keys()]).toEqual(["net"]);
  });
});
