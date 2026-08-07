// readonlyprobe is a developer-only hardware acceptance helper. It shares
// the production ProbeJob and accepts only a fixed serial port argument; it
// has no firmware package or write command path.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"clawmatemaker/internal/jobs"
)

func main() {
	port := flag.String("port", "", "serial port to probe read-only")
	logRoot := flag.String("log-root", "", "job-log root (default: OS temporary directory)")
	flag.Parse()
	if *port == "" {
		fmt.Fprintln(os.Stderr, "--port is required")
		os.Exit(2)
	}
	if *logRoot == "" {
		*logRoot = os.TempDir()
	}
	job, err := jobs.NewProbeJob(*logRoot, *port, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result, runErr := job.Run(context.Background())
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
	if runErr != nil {
		os.Exit(1)
	}
}
