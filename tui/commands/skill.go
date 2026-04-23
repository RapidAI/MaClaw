package commands

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// RunSkill 执行 skill 子命令。
func RunSkill(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui skill <list|add|delete|backup|restore|import|export>")
	}
	switch args[0] {
	case "list":
		return skillList(args[1:])
	case "add":
		return skillAdd(args[1:])
	case "delete":
		return skillDelete(args[1:])
	case "backup":
		return skillBackup(args[1:])
	case "restore":
		return skillRestore(args[1:])
	case "import":
		return skillImport(args[1:])
	case "export":
		return skillExport(args[1:])
	default:
		return NewUsageError("unknown skill action: %s", args[0])
	}
}

// localSkillInfo 本地技能信息（从 YAML 读取）。
type localSkillInfo struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers" json:"triggers"`
	Status      string   `yaml:"status" json:"status"`
}

func skillList(args []string) error {
	fs := flag.NewFlagSet("skill list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	// Merge config-based skills with file-based skills (same as GUI loadSkills).
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()

	known := make(map[string]bool)
	var skills []localSkillInfo

	// Config skills first (highest priority).
	for _, s := range cfg.NLSkills {
		skills = append(skills, localSkillInfo{
			Name:        s.Name,
			Description: s.Description,
			Triggers:    s.Triggers,
			Status:      s.Status,
		})
		known[s.Name] = true
	}

	// File-based skills from all scan roots.
	allFileSkills := skill.ScanAllSkillDirs()
	for _, s := range allFileSkills {
		if !known[s.Name] {
			skills = append(skills, localSkillInfo{
				Name:        s.Name,
				Description: s.Description,
				Triggers:    s.Triggers,
				Status:      s.Status,
			})
			known[s.Name] = true
		}
	}

	if *jsonOut {
		return PrintJSON(skills)
	}
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		roots := skill.SkillScanRoots()
		for _, r := range roots {
			fmt.Printf("  Scanned: %s\n", r)
		}
		return nil
	}
	fmt.Printf("%-20s %-8s %-30s %s\n", "NAME", "STATUS", "TRIGGERS", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 85))
	for _, s := range skills {
		triggers := strings.Join(s.Triggers, ", ")
		fmt.Printf("%-20s %-8s %-30s %s\n",
			TruncateDisplay(s.Name, 20),
			s.Status,
			TruncateDisplay(triggers, 30),
			TruncateDisplay(s.Description, 40))
	}
	return nil
}

func skillAdd(args []string) error {
	fs := flag.NewFlagSet("skill add", flag.ExitOnError)
	name := fs.String("name", "", "技能名称（必填）")
	desc := fs.String("desc", "", "技能描述")
	triggers := fs.String("triggers", "", "触发词（逗号分隔）")
	fs.Parse(args)

	if *name == "" {
		return NewUsageError("usage: skill add --name <name> [--desc <description>] [--triggers <t1,t2>]")
	}

	skillsRoot, err := skill.PrimarySkillsDir()
	if err != nil {
		return fmt.Errorf("cannot determine skills directory: %w", err)
	}
	skillDir := filepath.Join(skillsRoot, *name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}

	info := localSkillInfo{
		Name:        *name,
		Description: *desc,
		Status:      "active",
	}
	if *triggers != "" {
		info.Triggers = strings.Split(*triggers, ",")
	}

	data, err := skill.FormatSkillYAMLFile(&skill.SkillYAMLFile{
		Name:        info.Name,
		Description: info.Description,
		Triggers:    info.Triggers,
		Status:      info.Status,
	})
	if err != nil {
		return fmt.Errorf("format skill.yaml: %w", err)
	}
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := fileutil.AtomicWriteFile(yamlPath, data, 0o644); err != nil {
		return fmt.Errorf("write skill.yaml: %w", err)
	}
	fmt.Printf("Skill '%s' created at %s\n", *name, skillDir)
	return nil
}

func skillDelete(args []string) error {
	fs := flag.NewFlagSet("skill delete", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skill delete <name>")
	}
	name := fs.Arg(0)

	// Use ScanSkillDirAll — the unfiltered variant of the same scanner
	// that populates the skill list — so any format it can parse is
	// automatically covered and platform-incompatible skills can still
	// be deleted.
	for _, root := range skill.SkillScanRoots() {
		for _, s := range skill.ScanSkillDirAll(root) {
			if s.Name == name || s.DirName == name {
				if s.SkillDir != "" {
					if err := os.RemoveAll(s.SkillDir); err != nil {
						return fmt.Errorf("delete skill: %w", err)
					}
					fmt.Printf("Skill '%s' deleted from %s.\n", name, s.SkillDir)
					return nil
				}
			}
		}
	}

	return fmt.Errorf("skill '%s' not found in any skill directory", name)
}

func skillsRoot() (string, error) {
	return skill.PrimarySkillsDir()
}

func skillBackup(args []string) error {
	fs := flag.NewFlagSet("skill backup", flag.ExitOnError)
	outFile := fs.String("o", "", "输出 zip 路径（默认 skills_backup.zip）")
	fs.Parse(args)

	root, err := skillsRoot()
	if err != nil {
		return err
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return fmt.Errorf("skills directory does not exist: %s", root)
	}

	output := *outFile
	if output == "" {
		output = "skills_backup.zip"
	}

	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	count := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			return nil
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		writer, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		count++
		return err
	})
	if err != nil {
		return fmt.Errorf("zip skills: %w", err)
	}
	fmt.Printf("Skills backed up: %d files → %s\n", count, output)
	return nil
}

func skillRestore(args []string) error {
	fs := flag.NewFlagSet("skill restore", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: skill restore <backup.zip>")
	}
	zipPath := fs.Arg(0)

	root, err := skillsRoot()
	if err != nil {
		return err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	count := 0
	for _, f := range r.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		count++
	}
	fmt.Printf("Skills restored: %d files from %s\n", count, zipPath)
	return nil
}

func skillExport(args []string) error {
	fs := flag.NewFlagSet("skill export", flag.ExitOnError)
	outDir := fs.String("o", ".", "输出目录")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: skill export <name> [-o <output-dir>]")
	}
	name := fs.Arg(0)

	root, err := skillsRoot()
	if err != nil {
		return err
	}
	srcDir := filepath.Join(root, name)
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("skill '%s' not found at %s", name, srcDir)
	}

	zipPath := filepath.Join(*outDir, name+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	count := 0
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(name, rel))
		header.Method = zip.Deflate
		writer, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		count++
		return err
	})
	if err != nil {
		return fmt.Errorf("zip skill: %w", err)
	}
	fmt.Printf("Skill '%s' exported: %d files → %s\n", name, count, zipPath)
	return nil
}

func skillImport(args []string) error {
	fs := flag.NewFlagSet("skill import", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: skill import <skill.zip>")
	}
	zipPath := fs.Arg(0)

	root, err := skillsRoot()
	if err != nil {
		return err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	count := 0
	var skillName string
	for _, f := range r.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if skillName == "" {
			parts := strings.SplitN(filepath.ToSlash(f.Name), "/", 2)
			if len(parts) > 0 {
				skillName = parts[0]
			}
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		count++
	}
	fmt.Printf("Skill '%s' imported: %d files from %s\n", skillName, count, zipPath)
	return nil
}
