package domain

// UploadFolder is one top-level directory inside wp-content/uploads on the
// production server, with its size, so the user can pick what to pull instead
// of waiting for a whole media library.
type UploadFolder struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// MediaPullProgress is one step of a running media pull, sent as a Wails event.
type MediaPullProgress struct {
	Phase       string `json:"phase"` // "pull" | "done" | "error"
	Folder      string `json:"folder,omitempty"`
	Detail      string `json:"detail"`
	Bytes       int64  `json:"bytes,omitempty"`
	Files       int    `json:"files,omitempty"`
	FolderIndex int    `json:"folderIndex,omitempty"`
	FolderTotal int    `json:"folderTotal,omitempty"`
}

// MediaPullResult is the outcome of a completed media pull.
type MediaPullResult struct {
	Folders      []string `json:"folders"`
	FilesWritten int      `json:"filesWritten"`
	BytesWritten int64    `json:"bytesWritten"`
	LocalPath    string   `json:"localPath"`
	Warnings     []string `json:"warnings,omitempty"`
}
