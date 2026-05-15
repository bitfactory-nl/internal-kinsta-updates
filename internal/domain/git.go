package domain

import "time"

type GitStatus struct {
	Branch        string       `json:"branch"`
	Upstream      string       `json:"upstream"`
	Ahead         int          `json:"ahead"`
	Behind        int          `json:"behind"`
	Staged        []FileChange `json:"staged"`
	Unstaged      []FileChange `json:"unstaged"`
	Untracked     []string     `json:"untracked"`
	Conflicted    []string     `json:"conflicted"`
	HasSubmodules bool         `json:"hasSubmodules"`
	IsRepo        bool         `json:"isRepo"`
}

type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
	ChangeRenamed  ChangeKind = "renamed"
)

type FileChange struct {
	Path       string     `json:"path"`
	OldPath    string     `json:"oldPath,omitempty"`
	Kind       ChangeKind `json:"kind"`
	Insertions int        `json:"insertions"`
	Deletions  int        `json:"deletions"`
}

type Commit struct {
	Hash       string    `json:"hash"`
	ShortHash  string    `json:"shortHash"`
	Author     Person    `json:"author"`
	Committer  Person    `json:"committer"`
	Message    string    `json:"message"`
	Subject    string    `json:"subject"`
	Parents    []string  `json:"parents"`
	Refs       []string  `json:"refs"`
	AuthoredAt time.Time `json:"authoredAt"`
}

type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Branch struct {
	Name       string `json:"name"`
	FullRef    string `json:"fullRef"`
	IsRemote   bool   `json:"isRemote"`
	IsCurrent  bool   `json:"isCurrent"`
	Upstream   string `json:"upstream,omitempty"`
	HeadCommit string `json:"headCommit"`
}

type Tag struct {
	Name      string    `json:"name"`
	Annotated bool      `json:"annotated"`
	Message   string    `json:"message,omitempty"`
	Commit    string    `json:"commit"`
	TaggedAt  time.Time `json:"taggedAt"`
}

type Stash struct {
	Index     int       `json:"index"`
	Message   string    `json:"message"`
	Branch    string    `json:"branch"`
	StashedAt time.Time `json:"stashedAt"`
}

type FileDiff struct {
	Path    string     `json:"path"`
	OldPath string     `json:"oldPath,omitempty"`
	Binary  bool       `json:"binary"`
	Hunks   []DiffHunk `json:"hunks"`
}

type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

type DiffLineKind string

const (
	LineContext DiffLineKind = "context"
	LineAdd     DiffLineKind = "add"
	LineDel     DiffLineKind = "del"
)

type DiffLine struct {
	Kind    DiffLineKind `json:"kind"`
	Content string       `json:"content"`
	OldNum  int          `json:"oldNum,omitempty"`
	NewNum  int          `json:"newNum,omitempty"`
}

type BlameLine struct {
	LineNo  int       `json:"lineNo"`
	Commit  string    `json:"commit"`
	Author  Person    `json:"author"`
	Date    time.Time `json:"date"`
	Content string    `json:"content"`
}

type ProjectStatusSummary struct {
	ProjectID   string    `json:"projectId"`
	DisplayName string    `json:"displayName"`
	Branch      string    `json:"branch"`
	Ahead       int       `json:"ahead"`
	Behind      int       `json:"behind"`
	Dirty       bool      `json:"dirty"`
	IsRepo      bool      `json:"isRepo"`
}

type GraphCommit struct {
	Hash       string      `json:"hash"`
	ShortHash  string      `json:"shortHash"`
	Subject    string      `json:"subject"`
	Author     string      `json:"author"`
	AuthorDate time.Time   `json:"authorDate"`
	Parents    []string    `json:"parents"`
	Refs       []string    `json:"refs"`
	Lane       int         `json:"lane"`
	Columns    int         `json:"columns"`
	Edges      []GraphEdge `json:"edges"`
}

type GraphEdge struct {
	FromLane  int `json:"fromLane"`
	ToLane    int `json:"toLane"`
	ColorLane int `json:"colorLane"`
}
