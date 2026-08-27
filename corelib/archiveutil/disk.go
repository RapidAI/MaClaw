package archiveutil

// AvailableBytes reports free space on the volume containing path.
func AvailableBytes(path string) (int64, error) {
	return availableBytes(path)
}
