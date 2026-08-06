package domain

import "time"

// LogFile is one of the log files Kinsta exposes per environment.
type LogFile string

const (
	LogFileError     LogFile = "error"
	LogFileAccess    LogFile = "access"
	LogFileCachePerf LogFile = "kinsta-cache-perf"
)

// Valid reports whether f is a file name the Kinsta API accepts.
func (f LogFile) Valid() bool {
	switch f {
	case LogFileError, LogFileAccess, LogFileCachePerf:
		return true
	}
	return false
}

// LogKind is what a log line turned out to be after parsing. The distinction
// matters because a Kinsta error log is dominated by bot traffic: on a real
// sample 141 of 147 lines were scanners probing directories, and the few PHP
// fatals were bots requesting core files directly. Only some kinds can
// plausibly point at a bug in the project's own code.
type LogKind string

const (
	KindPHPFatal      LogKind = "php_fatal"
	KindPHPWarning    LogKind = "php_warning"
	KindPHPDeprecated LogKind = "php_deprecated"
	KindPHPNotice     LogKind = "php_notice"
	// KindPHPOther is a PHP message that is not one of the standard error
	// levels — a var_dump, a WP_Error print_r, a plugin writing to stderr.
	KindPHPOther LogKind = "php_other"
	// KindBotProbe is a scanner walking the filesystem: "directory index of
	// ... is forbidden" and friends. Never a code bug.
	KindBotProbe LogKind = "bot_probe"
	KindNginx    LogKind = "nginx"
	KindAccess   LogKind = "access"
	KindUnknown  LogKind = "unknown"
)

// Severity orders kinds for display: higher sorts first.
func (k LogKind) Severity() int {
	switch k {
	case KindPHPFatal:
		return 50
	case KindPHPWarning:
		return 40
	case KindPHPDeprecated, KindPHPNotice:
		return 30
	case KindPHPOther:
		return 25
	case KindNginx:
		return 20
	case KindUnknown:
		return 10
	case KindAccess:
		return 5
	case KindBotProbe:
		return 1
	}
	return 0
}

// IsPHP reports whether the kind came from PHP rather than from nginx itself.
func (k LogKind) IsPHP() bool {
	switch k {
	case KindPHPFatal, KindPHPWarning, KindPHPDeprecated, KindPHPNotice, KindPHPOther:
		return true
	}
	return false
}

// LogEntry is one parsed log line.
type LogEntry struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level"` // nginx level: error, warn, crit, ...
	Kind     LogKind   `json:"kind"`
	Message  string    `json:"message"` // the core message, unwrapped from nginx
	File     string    `json:"file"`    // absolute production path, when named
	Line     int       `json:"line"`
	Stack    string    `json:"stack"`
	ClientIP string    `json:"clientIp"`
	Request  string    `json:"request"` // e.g. GET /wp-settings.php HTTP/2.0
	Host     string    `json:"host"`
	Raw      string    `json:"raw"`
}

// LogGroup collects identical-looking entries. Grouping is the whole point of
// this feature: a log with 141 near-identical scanner lines is unreadable
// line-by-line but is four groups once fingerprinted.
type LogGroup struct {
	ID       string  `json:"id"` // fingerprint
	Kind     LogKind `json:"kind"`
	Severity int     `json:"severity"`
	Title    string  `json:"title"` // normalised message, safe to show

	File     string `json:"file"`     // production path as logged
	Line     int    `json:"line"`     // 0 when unknown
	RepoPath string `json:"repoPath"` // repo-relative path, empty when unmapped
	IsCore   bool   `json:"isCore"`   // WordPress core file rather than project code

	Count int       `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`

	Samples []LogEntry `json:"samples"` // capped, newest first

	// AIEligible gates the "fix with AI" button. AIReason always explains the
	// verdict — including when it is true — so the UI never shows a bare
	// disabled button.
	AIEligible bool   `json:"aiEligible"`
	AIReason   string `json:"aiReason"`

	// HasPII records that scrubbing found personal data in these lines.
	HasPII bool `json:"hasPii"`
}

// LogFetchResult is one retrieval of one log file.
type LogFetchResult struct {
	File           LogFile    `json:"file"`
	Environment    string     `json:"environment"`
	LinesRequested int        `json:"linesRequested"`
	LinesReceived  int        `json:"linesReceived"`
	Parsed         int        `json:"parsed"`
	Groups         []LogGroup `json:"groups"`
	FetchedAt      time.Time  `json:"fetchedAt"`
	Warnings       []string   `json:"warnings"`
}

// AIFixPhase names a step of the AI fix run, for progress events.
type AIFixPhase string

const (
	FixPhaseBranch  AIFixPhase = "branch"
	FixPhaseAI      AIFixPhase = "ai"
	FixPhaseGuard   AIFixPhase = "guard"
	FixPhaseLint    AIFixPhase = "lint"
	FixPhaseBuild   AIFixPhase = "build"
	FixPhaseCommit  AIFixPhase = "commit"
	FixPhasePush    AIFixPhase = "push"
	FixPhasePR      AIFixPhase = "pr"
	FixPhaseDone    AIFixPhase = "done"
	FixPhaseBlocked AIFixPhase = "blocked"
)

// AIFixProgress is emitted while a fix run is going, over the event channel
// logs:<projectID>:fix.
type AIFixProgress struct {
	GroupID string     `json:"groupId"`
	Phase   AIFixPhase `json:"phase"`
	Detail  string     `json:"detail"`
}

// AIFixResult reports what a fix run did. Committed/Pushed/PullRequestURL are
// only ever set when every guardrail passed; a blocked run leaves the branch in
// place with the changes uncommitted so they can be inspected.
type AIFixResult struct {
	GroupID string `json:"groupId"`
	Branch  string `json:"branch"`
	BaseRef string `json:"baseRef"`

	AISummary    string   `json:"aiSummary"`
	ChangedFiles []string `json:"changedFiles"`

	LintOutput string `json:"lintOutput"`
	// DiffStat is de `git show --stat` van de commit, zodat de omvang van de
	// wijziging in één oogopslag te zien is.
	DiffStat string `json:"diffStat"`

	Committed      bool   `json:"committed"`
	CommitHash     string `json:"commitHash"`
	Pushed         bool   `json:"pushed"`
	PullRequestURL string `json:"pullRequestUrl"`

	// Blocked means a guardrail stopped the run before commit. BlockReason is
	// then always filled in.
	Blocked     bool     `json:"blocked"`
	BlockReason string   `json:"blockReason"`
	Warnings    []string `json:"warnings"`

	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// AIFixPreview is what the tool would send to the AI, so it can be inspected
// before anything leaves the machine.
type AIFixPreview struct {
	GroupID  string   `json:"groupId"`
	Prompt   string   `json:"prompt"`
	Masked   []string `json:"masked"` // kinds of data that were masked
	RepoPath string   `json:"repoPath"`
	Branch   string   `json:"branch"`
}
