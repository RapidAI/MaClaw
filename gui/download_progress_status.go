package main

type downloadProgressStatus string

const (
	downloadProgressStatusDownloading downloadProgressStatus = "downloading"
	downloadProgressStatusVerifying   downloadProgressStatus = "verifying"
	downloadProgressStatusCompleted   downloadProgressStatus = "completed"
	downloadProgressStatusError       downloadProgressStatus = "error"
	downloadProgressStatusCancelled   downloadProgressStatus = "cancelled"
)
