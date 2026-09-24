export type UnauthorizedListener = () => void;

let listener: UnauthorizedListener | null = null;

export function setUnauthorizedListener(next: UnauthorizedListener | null) {
  listener = next;
}

export function notifyUnauthorized(): void {
  listener?.();
}
