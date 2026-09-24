export interface Health {
  ok: boolean;
  docker: boolean;
  image: boolean;
  image_name: string;
  instance: string;
  mem_available_mb: number;
  error: string;
}

export type LabMode = "tutorial" | "guided" | "real";

export const labModes: readonly LabMode[] = ["tutorial", "guided", "real"];

export interface TopicNode {
  id: string;
  title: string;
  labs: number;
  docs: number;
  children: TopicNode[];
}

export interface DocRef {
  id: string;
  title: string;
}

export interface LabSummary {
  id: string;
  title: string;
  topic: string;
  level: number;
  modes: LabMode[];
  estimated_minutes: number;
  related_docs: DocRef[];
  has_hidden_checkpoints: boolean;
}

export interface LabNode {
  name: string;
  role: string;
}

export interface LabCheckpoint {
  id: string;
  title: string;
}

export interface LabDetail extends LabSummary {
  nodes: LabNode[];
  checkpoints: LabCheckpoint[];
  topic_title: string;
}

export interface DocSummary {
  id: string;
  title: string;
  topic: string;
}

export interface Doc extends DocSummary {
  body: string;
  completed: boolean;
}

export interface TrackSummary {
  id: string;
  title: string;
  steps: number;
  completed: number;
}

export interface TrackStep {
  kind: "doc" | "lab";
  ref: string;
  title: string;
  mode: LabMode | null;
  completed: boolean;
}

export interface Track {
  id: string;
  title: string;
  steps: TrackStep[];
}

export interface ProgressRequest {
  kind: "doc";
  ref: string;
}

export type AttemptStatus =
  "provisioning" | "running" | "passed" | "abandoned" | "expired" | "error";

export type CheckpointStatus = "pending" | "pass" | "fail" | "error";

export interface AttemptCheckpoint {
  id: string;
  title: string;
  status: CheckpointStatus;
  first_passed_at: string | null;
}

export interface TutorialStep {
  checkpoint: string;
  instruction: string;
}

export interface Attempt {
  id: string;
  lab_id: string;
  mode: LabMode;
  status: AttemptStatus;
  error_message: string;
  lab: LabSummary;
  ticket: string;
  nodes: LabNode[];
  checkpoints: AttemptCheckpoint[];
  checkpoints_hidden: boolean;
  tutorial_steps: TutorialStep[] | null;
  submit_count: number;
  elapsed_ms: number;
  started_at: string | null;
  ended_at: string | null;
  server_time: string;
  created_at: string;
}

export interface SubmitCheckpoint {
  id: string;
  title: string;
  status: CheckpointStatus;
}

export interface SubmitResult {
  passed: boolean;
  checkpoints: SubmitCheckpoint[];
  hidden_failed: number;
  submit_count: number;
}

export interface ResultCheckpoint extends AttemptCheckpoint {
  visible: boolean;
}

export interface Result {
  attempt_id: string;
  status: AttemptStatus;
  lab: LabSummary;
  elapsed_ms: number;
  command_count: number;
  checkpoints: ResultCheckpoint[];
  submit_count: number;
  solution: string;
}

export type ProvisioningStep =
  "networks" | "containers" | "bootstrap" | "k3s" | "setup" | "precheck";

export const provisioningSteps: readonly ProvisioningStep[] = [
  "networks",
  "containers",
  "bootstrap",
  "k3s",
  "setup",
  "precheck",
];

export interface AttemptStatusEvent {
  type: "status";
  status: AttemptStatus;
  error_message: string;
  elapsed_ms: number;
  server_time: string;
}

export interface AttemptProvisioningEvent {
  type: "provisioning";
  step: ProvisioningStep;
  attempt?: number;
}

export interface AttemptCheckpointEvent {
  type: "checkpoint";
  id: string;
  status: CheckpointStatus;
  first_passed_at: string | null;
}

export interface AttemptTickEvent {
  type: "tick";
  elapsed_ms: number;
  server_time: string;
}

export interface AttemptSubmitEvent {
  type: "submit";
  passed: boolean;
  hidden_failed: number;
  submit_count: number;
}

export interface AttemptErrorEvent {
  type: "error";
  message: string;
}

export type AttemptEvent =
  | AttemptStatusEvent
  | AttemptProvisioningEvent
  | AttemptCheckpointEvent
  | AttemptTickEvent
  | AttemptSubmitEvent
  | AttemptErrorEvent;

export interface TerminalResizeMessage {
  type: "resize";
  cols: number;
  rows: number;
}

export interface TerminalExitMessage {
  type: "exit";
}

export interface TerminalErrorMessage {
  type: "error";
  message: string;
}

export type TerminalServerMessage = TerminalExitMessage | TerminalErrorMessage;

export interface CreateAttemptRequest {
  lab_id: string;
  mode: LabMode;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}

export type UserRole = "admin" | "user";

export const userRoles: readonly UserRole[] = ["admin", "user"];

export type UserLocale = "zh" | "en";

export interface Me {
  id: string;
  username: string;
  role: UserRole;
  locale: UserLocale;
  created_at: string;
}

export interface AdminUser extends Me {
  disabled_at: string | null;
  attempts: number;
}

export interface AdminStats {
  sandboxes_active: number;
  sandboxes_max: number;
  recordings_bytes: number;
  attempts: number;
}

export interface AttemptUser {
  id: string;
  username: string;
}

export interface AdminAttempt {
  id: string;
  lab: LabSummary;
  mode: LabMode;
  status: AttemptStatus;
  elapsed_ms: number;
  created_at: string;
  user: AttemptUser;
}

export interface RunnerBusy {
  sandboxes_active: number;
  sandboxes_max: number;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface UpdateMeRequest {
  locale: UserLocale;
}

export interface CreateUserRequest {
  username: string;
  password: string;
  role: UserRole;
}

export interface UpdateUserRequest {
  role?: UserRole;
  disabled?: boolean;
  password?: string;
}

export interface HistoryItem {
  id: string;
  lab: LabSummary;
  mode: LabMode;
  status: AttemptStatus;
  elapsed_ms: number;
  submit_count: number;
  command_count: number;
  created_at: string;
  ended_at: string | null;
  user: AttemptUser;
}

export type HistoryQuery = {
  user_id?: string;
  limit?: number;
  before?: string;
};

export interface CommandEntry {
  id: string;
  node: string;
  ts: string;
  user: string;
  cwd: string;
  command: string;
  exit_code: number;
}

export type CommandsQuery = {
  limit?: number;
  after?: string;
};

export interface RecordingInfo {
  id: string;
  node: string;
  tab: string;
  started_at: string;
  ended_at: string | null;
  bytes: number;
}
