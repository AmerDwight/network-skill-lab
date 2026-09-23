export interface Health {
  ok: boolean;
  docker: boolean;
  image: boolean;
  image_name: string;
  mem_available_mb: number;
  error: string;
}

export type LabMode = "guided";

export interface LabSummary {
  id: string;
  title: string;
  topic: string;
  level: number;
  modes: LabMode[];
  estimated_minutes: number;
}

export interface LabNode {
  name: string;
  role: string;
}

export interface LabCheckpoint {
  id: string;
  title: string;
}

export interface Lab extends LabSummary {
  nodes: LabNode[];
  checkpoints: LabCheckpoint[];
}

export type AttemptStatus =
  "provisioning" | "running" | "passed" | "abandoned" | "expired" | "error";

export interface Attempt {
  id: string;
  lab_id: string;
  mode: string;
  status: AttemptStatus;
  elapsed_ms: number;
  server_time: string;
  created_at: string;
}

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
