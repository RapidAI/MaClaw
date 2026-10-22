package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// "maclawsrv runs ..." queries the desktop run event stream from a shell on
// the host (docs/desktop-observability.md). It only reads files.
//
//	runs list     [--since 24h] [--bot B] [--tenant T] [--limit 20]
//	runs tail     <run_id> [-n 50] [--json]
//	runs failures [--since 24h] [--bot B]
//	runs find     [--bot B] [--since 24h] [--until RFC3339] [--result CLASS] [--action A]
func desktopRunsMain(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: maclawsrv runs list|tail|failures|find [flags]   (--root DIR overrides "+desktopEventsDir()+")")
		return 2
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("runs "+cmd, flag.ContinueOnError)
	fs.SetOutput(stdout)
	root := fs.String("root", desktopEventsDir(), "event directory")
	since := fs.Duration("since", 24*time.Hour, "look back this far")
	until := fs.String("until", "", "RFC3339 upper bound")
	bot := fs.String("bot", "", "bot id (or owner)")
	tenant := fs.String("tenant", "", "tenant id")
	limit := fs.Int("limit", 20, "max runs")
	n := fs.Int("n", 50, "tail lines")
	asJSON := fs.Bool("json", false, "raw JSON lines")
	result := fs.String("result", "", "result class filter")
	action := fs.String("action", "", "action filter")
	// Flags may follow the positional run id.
	var positional []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	upper := time.Now().Add(time.Minute)
	if *until != "" {
		t, err := time.Parse(time.RFC3339, *until)
		if err != nil {
			fmt.Fprintln(stdout, "bad --until:", err)
			return 2
		}
		upper = t
	}
	lower := upper.Add(-*since)
	runs := desktopRunFiles(*root, *tenant, *bot, lower, upper)
	switch cmd {
	case "list":
		if len(runs) > *limit {
			runs = runs[:*limit]
		}
		fmt.Fprintf(stdout, "%-25s %-10s %-18s %-36s %5s %s\n", "last", "tenant", "bot", "run_id", "steps", "results")
		for _, r := range runs {
			s := desktopSummarizeRun(r.path)
			fmt.Fprintf(stdout, "%-25s %-10s %-18s %-36s %5d %s\n", r.mod.Format(time.RFC3339), r.tenant, r.bot, r.run, s.steps, s.classes())
		}
	case "tail":
		if len(positional) == 0 {
			fmt.Fprintln(stdout, "usage: maclawsrv runs tail <run_id> [-n 50] [--json]")
			return 2
		}
		path := desktopFindRun(*root, positional[0])
		if path == "" {
			fmt.Fprintln(stdout, "run not found:", positional[0])
			return 1
		}
		lines := desktopReadLines(path)
		if len(lines) > *n {
			lines = lines[len(lines)-*n:]
		}
		for _, line := range lines {
			if *asJSON {
				fmt.Fprintln(stdout, line)
				continue
			}
			fmt.Fprintln(stdout, desktopEventPretty(line))
		}
	case "failures":
		counts := map[string]int{}
		examples := map[string]string{}
		for _, r := range runs {
			for _, line := range desktopReadLines(r.path) {
				var ev desktopEvent
				if json.Unmarshal([]byte(line), &ev) != nil || ev.Event != "step" || ev.Result == "ok" {
					continue
				}
				k := ev.Result + " " + ev.Action
				counts[k]++
				examples[k] = r.run + " step " + fmt.Sprint(ev.Step) + ": " + ev.Error
			}
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
		fmt.Fprintf(stdout, "failures in %d run(s) since %s\n", len(runs), lower.Format(time.RFC3339))
		for _, k := range keys {
			fmt.Fprintf(stdout, "%5d  %-40s e.g. %s\n", counts[k], k, desktopStepClip(examples[k], 140))
		}
	case "find":
		for _, r := range runs {
			for _, line := range desktopReadLines(r.path) {
				var ev desktopEvent
				if json.Unmarshal([]byte(line), &ev) != nil {
					continue
				}
				if (*result != "" && ev.Result != *result) || (*action != "" && ev.Action != *action) {
					continue
				}
				fmt.Fprintln(stdout, desktopEventPretty(line))
				if *result == "" && *action == "" {
					break // one line per run when not filtering events
				}
			}
		}
	default:
		fmt.Fprintln(stdout, "unknown command", cmd)
		return 2
	}
	return 0
}

type desktopRunFile struct {
	path, tenant, bot, run string
	mod                    time.Time
}

func desktopRunFiles(root, tenant, bot string, lower, upper time.Time) []desktopRunFile {
	var out []desktopRunFile
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(lower) || info.ModTime().After(upper) {
			return nil
		}
		if (tenant != "" && parts[0] != tenant) || (bot != "" && parts[1] != bot) {
			return nil
		}
		out = append(out, desktopRunFile{path, parts[0], parts[1], strings.TrimSuffix(parts[2], ".jsonl"), info.ModTime()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

func desktopFindRun(root, run string) string {
	matches, _ := filepath.Glob(filepath.Join(root, "*", "*", desktopEventSafe(run)+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func desktopReadLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type desktopRunSummary struct {
	steps int
	byRes map[string]int
}

func (s desktopRunSummary) classes() string {
	keys := make([]string, 0, len(s.byRes))
	for k := range s.byRes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, s.byRes[k]))
	}
	return strings.Join(parts, " ")
}

func desktopSummarizeRun(path string) desktopRunSummary {
	s := desktopRunSummary{byRes: map[string]int{}}
	for _, line := range desktopReadLines(path) {
		var ev desktopEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Event == "step" {
			s.steps++
			s.byRes[ev.Result]++
			delete(s.byRes, "step_start")
		} else if ev.Event == "step_start" {
			// Only a start without its step (a hung or running call) counts.
			s.byRes["step_start"] = 1
		} else {
			s.byRes[ev.Event]++
		}
	}
	return s
}

func desktopEventPretty(line string) string {
	var ev desktopEvent
	if json.Unmarshal([]byte(line), &ev) != nil {
		return line
	}
	ts := ev.TS
	if t, err := time.Parse(time.RFC3339Nano, ev.TS); err == nil {
		ts = t.Format("01-02 15:04:05")
	}
	if ev.Event == "step_start" {
		return fmt.Sprintf("%s #%d %-12s started site=%s model=%s", ts, ev.Step, ev.Action, ev.Site, ev.Model)
	}
	if ev.Event != "step" {
		return fmt.Sprintf("%s %s run=%s bot=%s lease=%s %s", ts, ev.Event, ev.RunID, ev.Bot, ev.Lease, ev.Reason)
	}
	args := make([]string, 0, len(ev.Args))
	for k, v := range ev.Args {
		args = append(args, k+"="+v)
	}
	sort.Strings(args)
	s := fmt.Sprintf("%s #%d %-12s %-18s think=%dms exec=%dms img=%d act=%d/%d site=%s model=%s {%s}", ts, ev.Step, ev.Action, ev.Result,
		ev.ThinkMS, ev.ExecMS, ev.ImageTokens, ev.Actions, ev.MaxActions, ev.Site, ev.Model, strings.Join(args, " "))
	if ev.Error != "" {
		s += " err=" + ev.Error
	}
	if ev.Trajectory != "" {
		s += " traj=" + ev.Trajectory
	}
	return s
}
