package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

var version = "dev"
var windowsGUI = "false"

type brandOption struct {
	ID          string
	ProductName string
	Label       string
	Aliases     []string
}

type installOptions struct {
	Brand          brandOption
	CurrentVersion string
	CheckOnly      bool
	NoLaunch       bool
	WaitInstaller  bool
	Progress       progressFunc
	Log            func(string)
}

type installResult struct {
	Release        latestReleaseInfo
	TargetFileName string
	DownloadedPath string
	Skipped        bool
}

var brandOptions = []brandOption{
	{ID: "maclaw", ProductName: "MaClaw", Label: "MaClaw (\u539f\u5382\u54c1\u724c)", Aliases: []string{"", "maclaw", "ma", "default"}},
	{ID: "qianxin", ProductName: "TigerClaw", Label: "TigerClaw (\u5947\u5b89\u4fe1 OEM \u7248)", Aliases: []string{"qianxin", "tigerclaw", "tiger", "\u5947\u5b89\u4fe1"}},
}

func main() {
	brandFlag := flag.String("brand", "maclaw", "product brand: maclaw or tigerclaw")
	currentFlag := flag.String("current-version", "", "optional current version; skips install if latest is not newer")
	checkOnly := flag.Bool("check", false, "check latest version and exit")
	noLaunch := flag.Bool("no-launch", false, "download only, do not launch installer")
	modeFlag := flag.String("mode", "auto", "run mode: auto, gui, tui, or cli")
	languageFlag := flag.String("lang", "auto", "display language: auto, zh, or en")
	guiMode := flag.Bool("gui", false, "run in GUI mode")
	tuiMode := flag.Bool("tui", false, "run in terminal UI mode")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()
	if err := setLanguage(*languageFlag); err != nil {
		exitf("%v", err)
	}

	if *showVersion {
		fmt.Printf("Ins-maclaw %s (%s)\n", version, languageName())
		return
	}

	brand, err := resolveBrand(*brandFlag)
	if err != nil {
		exitf("%v", err)
	}

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if *guiMode {
		mode = "gui"
	}
	if *tuiMode {
		mode = "tui"
	}
	if mode == "" || mode == "auto" {
		mode = defaultRunMode()
	}
	prepareConsoleForMode(mode)
	switch mode {
	case "gui":
		if err := runGUI(brand, *currentFlag, *checkOnly, *noLaunch); err != nil {
			exitf("%v", err)
		}
	case "tui":
		if err := runTUI(brand, *currentFlag, *checkOnly, *noLaunch); err != nil {
			exitf("%v", err)
		}
	case "cli":
		if err := runCLI(brand, *currentFlag, *checkOnly, *noLaunch); err != nil {
			exitf("%v", err)
		}
	default:
		exitf("unknown mode %q; supported: auto, gui, tui, cli", mode)
	}
}

func runCLI(brand brandOption, currentVersion string, checkOnly, noLaunch bool) error {
	lastShown := int64(-1)
	_, err := runInstall(context.Background(), installOptions{
		Brand:          brand,
		CurrentVersion: currentVersion,
		CheckOnly:      checkOnly,
		NoLaunch:       noLaunch,
		WaitInstaller:  true,
		Log:            func(msg string) { fmt.Println(msg) },
		Progress: func(downloaded, total int64) {
			if total > 0 {
				pct := downloaded * 100 / total
				if pct != lastShown && (pct == 100 || pct-lastShown >= 5) {
					lastShown = pct
					fmt.Printf(tr("downloading.percent")+"\n", pct, humanBytes(downloaded), humanBytes(total))
				}
				return
			}
			if downloaded-lastShown >= 10*1024*1024 || lastShown < 0 {
				lastShown = downloaded
				fmt.Printf(tr("downloading.bytes")+"\n", humanBytes(downloaded))
			}
		},
	})
	return err
}

func runTUI(defaultBrand brandOption, currentVersion string, checkOnly, noLaunch bool) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Ins-maclaw %s (%s)\n", version, languageName())
	fmt.Println(tr("tui.choose"))
	for i, brand := range brandOptions {
		mark := " "
		if brand.ID == defaultBrand.ID {
			mark = "*"
		}
		fmt.Printf("  %d. [%s] %s\n", i+1, mark, brandLabel(brand))
	}
	defaultIndex := brandSelectionIndex(defaultBrand)
	fmt.Printf(tr("tui.select"), defaultIndex)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	brand := defaultBrand
	if line == "" {
		brand = defaultBrand
	} else if line == "1" {
		brand = brandOptions[0]
	} else if line == "2" {
		brand = brandOptions[1]
	} else if resolved, err := resolveBrand(line); err == nil {
		brand = resolved
	} else {
		return fmt.Errorf(tr("invalid.selection"), line)
	}
	if !checkOnly {
		fmt.Printf(tr("tui.install.confirm"), brandLabel(brand))
		line, _ = reader.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "n" || line == "no" {
			fmt.Println(tr("tui.cancelled"))
			return nil
		}
	}
	return runCLI(brand, currentVersion, checkOnly, noLaunch)
}

func brandSelectionIndex(defaultBrand brandOption) int {
	for i, brand := range brandOptions {
		if brand.ID == defaultBrand.ID {
			return i + 1
		}
	}
	return 1
}

func runInstall(ctx context.Context, opts installOptions) (installResult, error) {
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}
	targetFileName, err := targetAssetName(opts.Brand.ProductName)
	if err != nil {
		return installResult{}, err
	}
	log(fmt.Sprintf(tr("cli.product"), brandLabel(opts.Brand)))
	log(fmt.Sprintf(tr("cli.target"), runtime.GOOS, runtime.GOARCH, targetFileName))
	log(tr("cli.checking"))
	release, err := fetchLatestReleaseFast(ctx, opts.Brand.ProductName, targetFileName)
	if err != nil {
		return installResult{}, fmt.Errorf("check failed: %w", err)
	}
	log(fmt.Sprintf(tr("cli.latest"), displayVersion(release.TagName), release.Source))
	result := installResult{Release: release, TargetFileName: targetFileName}
	if opts.CurrentVersion != "" && compareVersions(release.TagName, opts.CurrentVersion) <= 0 {
		log(fmt.Sprintf(tr("cli.uptodate"), opts.CurrentVersion, displayVersion(release.TagName)))
		result.Skipped = true
		return result, nil
	}
	if opts.CheckOnly {
		return result, nil
	}
	log(tr("cli.downloading"))
	path, err := downloadInstaller(ctx, opts.Brand.ProductName, targetFileName, release.DownloadURL, release.SHA256, opts.Progress)
	if err != nil {
		return result, fmt.Errorf("download failed: %w", err)
	}
	result.DownloadedPath = path
	log(fmt.Sprintf(tr("cli.downloaded"), path))
	if opts.NoLaunch {
		return result, nil
	}
	log(tr("cli.launching"))
	if err := launchInstaller(path, opts.WaitInstaller); err != nil {
		return result, fmt.Errorf("launch failed: %w", err)
	}
	time.Sleep(300 * time.Millisecond)
	return result, nil
}

func resolveBrand(input string) (brandOption, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	for _, brand := range brandOptions {
		for _, alias := range brand.Aliases {
			if input == alias {
				return brand, nil
			}
		}
	}
	return brandOption{}, fmt.Errorf(tr("invalid.brand"), input)
}

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
