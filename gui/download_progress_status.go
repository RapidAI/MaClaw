package main

type downloadProgressStatus string

const (
	downloadProgressStatusDownloading downloadProgressStatus = "downloading"
	downloadProgressStatusCompleted   downloadProgressStatus = "completed"
	downloadProgressStatusError       downloadProgressStatus = "error"
	downloadProgressStatusCancelled   downloadProgressStatus = "cancelled"
)
