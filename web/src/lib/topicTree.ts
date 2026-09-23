import type { TopicNode } from "../api/types";

export function totalLabs(topics: readonly TopicNode[]): number {
  return topics.reduce((total, topic) => total + topic.labs, 0);
}

export function topicPath(
  topics: readonly TopicNode[],
  id: string | null,
): TopicNode[] {
  if (id === null) {
    return [];
  }
  for (const topic of topics) {
    if (topic.id === id) {
      return [topic];
    }
    const below = topicPath(topic.children, id);
    if (below.length > 0) {
      return [topic, ...below];
    }
  }
  return [];
}

export function ancestorIds(
  topics: readonly TopicNode[],
  id: string | null,
): string[] {
  return topicPath(topics, id)
    .slice(0, -1)
    .map((topic) => topic.id);
}

export function expandedTopics(
  topics: readonly TopicNode[],
  selected: string | null,
  overrides: ReadonlyMap<string, boolean>,
): Set<string> {
  const expanded = new Set(ancestorIds(topics, selected));
  for (const [id, open] of overrides) {
    if (open) {
      expanded.add(id);
    } else {
      expanded.delete(id);
    }
  }
  return expanded;
}

export function toggleTopic(
  overrides: ReadonlyMap<string, boolean>,
  id: string,
  open: boolean,
): Map<string, boolean> {
  return new Map(overrides).set(id, open);
}
