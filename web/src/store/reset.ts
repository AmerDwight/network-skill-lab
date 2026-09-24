import { useAdminStore } from "./admin";
import { useAppStore } from "./app";
import { useAttemptStore } from "./attempt";
import { useDocsStore } from "./docs";
import { useHistoryStore } from "./history";
import { useTracksStore } from "./tracks";

export function resetUserScopedStores(): void {
  useAppStore.getState().reset();
  useAttemptStore.getState().reset();
  useAdminStore.getState().reset();
  useDocsStore.getState().reset();
  useHistoryStore.getState().reset();
  useTracksStore.getState().reset();
}
