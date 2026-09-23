package store

import "time"

const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusPassed       = "passed"
	StatusAbandoned    = "abandoned"
	StatusExpired      = "expired"
	StatusError        = "error"
)

const (
	CheckpointPending = "pending"
	CheckpointPass    = "pass"
	CheckpointFail    = "fail"
	CheckpointError   = "error"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

const LocalUsername = "local"

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Locale       string
	CreatedAt    time.Time
}

type Attempt struct {
	ID           string
	UserID       string
	LabID        string
	LabVersion   int
	CaseID       string
	Mode         string
	ParamsJSON   string
	Status       string
	ErrorMessage string
	StartedAt    *time.Time
	EndedAt      *time.Time
	ElapsedMS    int64
	RunnerID     string
	SandboxID    string
	CreatedAt    time.Time
}

type CheckpointRun struct {
	AttemptID     string
	CheckpointID  string
	FirstPassedAt *time.Time
	LastStatus    string
	LastRunAt     *time.Time
}

type CommandEntry struct {
	ID        int64
	AttemptID string
	Node      string
	TS        time.Time
	User      string
	CWD       string
	Command   string
	ExitCode  int
}

type Recording struct {
	ID        string
	AttemptID string
	Node      string
	TabID     string
	Path      string
	StartedAt time.Time
	EndedAt   *time.Time
}
