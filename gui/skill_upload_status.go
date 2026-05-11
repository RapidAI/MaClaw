package main

type skillUploadStatus string

const (
	skillUploadStatusPending   skillUploadStatus = "pending"
	skillUploadStatusUploading skillUploadStatus = "uploading"
	skillUploadStatusUploaded  skillUploadStatus = "uploaded"
	skillUploadStatusBlocked   skillUploadStatus = "blocked"
	skillUploadStatusFailed    skillUploadStatus = "failed"
)

func (s skillUploadStatus) String() string {
	return string(s)
}

func (s skillUploadStatus) IsPending() bool {
	return s == skillUploadStatusPending
}

func (s skillUploadStatus) IsUploading() bool {
	return s == skillUploadStatusUploading
}

func (s skillUploadStatus) IsUploaded() bool {
	return s == skillUploadStatusUploaded
}

func (s skillUploadStatus) IsFailed() bool {
	return s == skillUploadStatusFailed
}

func (s skillUploadStatus) IsBlocked() bool {
	return s == skillUploadStatusBlocked
}
