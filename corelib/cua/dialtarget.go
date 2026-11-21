package cua

// pipeTarget is the string CreateFileW receives: the pipe namespace plus
// the pipe's name. The namespace is written as bytes so its doubled
// leading separator cannot be collapsed by a literal.
func pipeTarget(addr string) string {
	sep := string([]byte{92})
	return sep + sep + "." + sep + "pipe" + sep + pipeName(addr)
}
