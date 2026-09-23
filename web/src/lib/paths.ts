import type { LabMode, TrackStep } from "../api/types";

export function encodePathSegments(id: string): string {
  return id.split("/").map(encodeURIComponent).join("/");
}

export function topicHref(topicId: string | null): string {
  if (topicId === null) {
    return "/";
  }
  return `/?topic=${encodeURIComponent(topicId)}`;
}

export function labHref(labId: string, mode?: LabMode | null): string {
  const path = `/labs/${encodeURIComponent(labId)}`;
  if (mode === undefined || mode === null) {
    return path;
  }
  return `${path}?mode=${encodeURIComponent(mode)}`;
}

export function docHref(docId: string): string {
  return `/docs/${encodePathSegments(docId)}`;
}

export function stepHref(step: TrackStep): string {
  return step.kind === "doc" ? docHref(step.ref) : labHref(step.ref, step.mode);
}
