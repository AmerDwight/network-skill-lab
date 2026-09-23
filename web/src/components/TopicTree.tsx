import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";

import type { TopicNode } from "../api/types";
import { topicHref } from "../lib/paths";
import { expandedTopics, toggleTopic, totalLabs } from "../lib/topicTree";

function TopicLink({
  id,
  title,
  labs,
  selected,
}: {
  id: string | null;
  title: string;
  labs: number;
  selected: boolean;
}) {
  return (
    <Link
      className={
        selected
          ? "topic-tree__link topic-tree__link--selected"
          : "topic-tree__link"
      }
      to={topicHref(id)}
      aria-current={selected ? "page" : undefined}
    >
      <span className="topic-tree__title">{title}</span>
      <span className="topic-tree__count">{labs}</span>
    </Link>
  );
}

function TopicItem({
  topic,
  selected,
  expanded,
  onToggle,
}: {
  topic: TopicNode;
  selected: string | null;
  expanded: ReadonlySet<string>;
  onToggle: (id: string) => void;
}) {
  const { t } = useTranslation();
  const open = expanded.has(topic.id);

  return (
    <li className="topic-tree__item">
      <div className="topic-tree__row">
        {topic.children.length > 0 ? (
          <button
            type="button"
            className="topic-tree__toggle"
            aria-expanded={open}
            aria-label={t(open ? "topics.collapse" : "topics.expand", {
              topic: topic.title,
            })}
            onClick={() => onToggle(topic.id)}
          >
            {open ? "▾" : "▸"}
          </button>
        ) : (
          <span className="topic-tree__toggle" />
        )}
        <TopicLink
          id={topic.id}
          title={topic.title}
          labs={topic.labs}
          selected={selected === topic.id}
        />
      </div>
      {topic.children.length > 0 && open ? (
        <ul className="topic-tree__children">
          {topic.children.map((child) => (
            <TopicItem
              key={child.id}
              topic={child}
              selected={selected}
              expanded={expanded}
              onToggle={onToggle}
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

export function TopicTree({
  topics,
  selected,
}: {
  topics: TopicNode[];
  selected: string | null;
}) {
  const { t } = useTranslation();
  const [overrides, setOverrides] = useState<ReadonlyMap<string, boolean>>(
    new Map(),
  );
  const expanded = expandedTopics(topics, selected, overrides);

  return (
    <ul className="topic-tree">
      <li className="topic-tree__item">
        <div className="topic-tree__row">
          <span className="topic-tree__toggle" />
          <TopicLink
            id={null}
            title={t("topics.all")}
            labs={totalLabs(topics)}
            selected={selected === null}
          />
        </div>
      </li>
      {topics.map((topic) => (
        <TopicItem
          key={topic.id}
          topic={topic}
          selected={selected}
          expanded={expanded}
          onToggle={(id) =>
            setOverrides((current) =>
              toggleTopic(current, id, !expanded.has(id)),
            )
          }
        />
      ))}
    </ul>
  );
}
