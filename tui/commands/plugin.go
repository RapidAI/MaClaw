package commands

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/RapidAI/CodeClaw/corelib/plugin"
)

// DefaultPluginRegistry is the package-level PluginRegistry instance.
// It must be set by the application startup code before CLI commands are used.
var DefaultPluginRegistry *plugin.PluginRegistry

// RunPlugin executes the plugin sub-command.
func RunPlugin(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui plugin <list|info|enable|disable>")
	}
	switch args[0] {
	case "list":
		return pluginList(args[1:])
	case "info":
		return pluginInfo(args[1:])
	case "enable":
		return pluginEnable(args[1:])
	case "disable":
		return pluginDisable(args[1:])
	default:
		return NewUsageError("unknown plugin action: %s", args[0])
	}
}

func pluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	plugins := DefaultPluginRegistry.List()

	if *jsonOut {
		return PrintJSON(plugins)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSTATUS\tTOOLS")
	for _, p := range plugins {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Type, p.Status, p.ToolCount)
	}
	return w.Flush()
}

func pluginInfo(args []string) error {
	fs := flag.NewFlagSet("plugin info", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin info <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	info, ok := DefaultPluginRegistry.Get(name)
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", info.Name)
	fmt.Fprintf(w, "Version:\t%s\n", info.Version)
	fmt.Fprintf(w, "Description:\t%s\n", info.Description)
	fmt.Fprintf(w, "Type:\t%s\n", info.Type)
	fmt.Fprintf(w, "Scope:\t%s\n", info.Scope)
	fmt.Fprintf(w, "Status:\t%s\n", info.Status)
	fmt.Fprintf(w, "Tools:\t%d\n", info.ToolCount)
	fmt.Fprintf(w, "Hooks:\t%d\n", info.HookCount)
	fmt.Fprintf(w, "Health:\t%s\n", info.Health.Status)
	if info.Error != "" {
		fmt.Fprintf(w, "Error:\t%s\n", info.Error)
	}
	return w.Flush()
}

func pluginEnable(args []string) error {
	fs := flag.NewFlagSet("plugin enable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin enable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	// Placeholder: real implementation requires a running registry with Start support.
	fmt.Printf("plugin %q enabled\n", name)
	return nil
}

func pluginDisable(args []string) error {
	fs := flag.NewFlagSet("plugin disable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin disable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	// Placeholder: real implementation requires a running registry with Stop support.
	fmt.Printf("plugin %q disabled\n", name)
	return nil
}
