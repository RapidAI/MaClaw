package skill

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExtractScriptParamsPythonArgparseRequiredAndOptional(t *testing.T) {
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("--input", required=True)
parser.add_argument("--format", default="pdf")
parser.add_argument("--verbose", action="store_true")
args = parser.parse_args()
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]bool{}
	required := map[string]bool{}
	cliFlag := map[string]string{}
	for _, param := range params {
		byName[param.Name] = true
		required[param.Name] = param.Required
		cliFlag[param.Name] = param.CLIFlag
	}

	if !byName["input"] || !required["input"] || cliFlag["input"] != "--input" {
		t.Fatalf("input param not extracted as required CLI option: %#v", params)
	}
	if !byName["format"] || required["format"] || cliFlag["format"] != "--format" {
		t.Fatalf("format param not extracted as optional CLI option: %#v", params)
	}
	if byName["verbose"] {
		t.Fatalf("store_true flag should not become a value param: %#v", params)
	}
}

func TestExtractScriptParamsPythonArgparsePositionals(t *testing.T) {
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("source-file")
parser.add_argument("output")
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]bool{}
	for _, param := range params {
		byName[param.Name] = param.Required && param.CLIFlag == ""
	}

	if !byName["source_file"] || !byName["output"] {
		t.Fatalf("positional argparse params = %#v, want required source_file and output without CLI flags", params)
	}
}

func TestExtractScriptParamsPythonArgparseDefaultsAndOptionalPositional(t *testing.T) {
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("action", nargs="?", default="realtime")
parser.add_argument("--lat", type=float, default=39.9)
parser.add_argument("--lng", type=float, default=116.4)
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if byName["action"].Required || byName["action"].Default != "realtime" {
		t.Fatalf("action param = %#v, want optional positional with realtime default", byName["action"])
	}
	if byName["lat"].Required || byName["lat"].Default != "39.9" || byName["lat"].CLIFlag != "--lat" {
		t.Fatalf("lat param = %#v, want optional --lat with numeric default", byName["lat"])
	}
	if byName["lng"].Required || byName["lng"].Default != "116.4" || byName["lng"].CLIFlag != "--lng" {
		t.Fatalf("lng param = %#v, want optional --lng with numeric default", byName["lng"])
	}
}

func TestExtractScriptParamsPythonClickOptionsAndArguments(t *testing.T) {
	script := `
import click

@click.command()
@click.argument("source-file")
@click.option("--format", required=True)
@click.option("--verbose", is_flag=True)
def cli(source_file, format, verbose):
    pass
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["source_file"].Required || byName["source_file"].CLIFlag != "" {
		t.Fatalf("Click params = %#v, want required positional source_file", params)
	}
	if !byName["format"].Required || byName["format"].CLIFlag != "--format" {
		t.Fatalf("Click params = %#v, want required format option", params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("Click params = %#v, is_flag option should not require a value", params)
	}
}

func TestExtractScriptParamsPythonTyperOptionAndArgument(t *testing.T) {
	script := `
import typer

def main(
    input_file: str = typer.Argument(...),
    target_lang: str = typer.Option("English", "--target-lang"),
    force: bool = typer.Option(False, "--force"),
):
    pass

typer.run(main)
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["input_file"].Required || byName["input_file"].CLIFlag != "" {
		t.Fatalf("Typer params = %#v, want required positional input_file", params)
	}
	if byName["target_lang"].Required || byName["target_lang"].CLIFlag != "--target-lang" {
		t.Fatalf("Typer params = %#v, want optional target_lang option", params)
	}
	if _, ok := byName["force"]; ok {
		t.Fatalf("Typer params = %#v, bool option should not require a value", params)
	}
}

func TestExtractScriptParamsPythonTyperRunSignature(t *testing.T) {
	script := `
import typer

def main(input_file: str, output: str, format: str = "pdf", verbose: bool = False):
    pass

typer.run(main)
`

	params := ExtractScriptParams(script, "python")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["input_file"].Required || byName["input_file"].CLIFlag != "" {
		t.Fatalf("Typer run params = %#v, want required input_file argument", params)
	}
	if !byName["output"].Required || byName["output"].CLIFlag != "" {
		t.Fatalf("Typer run params = %#v, want required output argument", params)
	}
	if byName["format"].Required || byName["format"].CLIFlag != "--format" {
		t.Fatalf("Typer run params = %#v, want optional format option", params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("Typer run params = %#v, bool default should not become value param", params)
	}
}

func TestExtractScriptParamsNodeArgvSliceDestructure(t *testing.T) {
	script := `
const [inputFile, output] = process.argv.slice(2)
console.log(inputFile, output)
`

	params := ExtractScriptParams(script, "node")
	byName := map[string]bool{}
	for _, param := range params {
		byName[param.Name] = param.Required && param.CLIFlag == ""
	}

	if !byName["input_file"] || !byName["output"] {
		t.Fatalf("node argv slice params = %#v, want required input_file and output", params)
	}
}

func TestExtractScriptParamsNodeCommanderOptions(t *testing.T) {
	script := `
const { program } = require('commander')
program
  .requiredOption('--input-file <path>')
  .option('--format <type>', 'output format')
  .option('--verbose', 'verbose logging')
`

	params := ExtractScriptParams(script, "node")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["input_file"].Required || byName["input_file"].CLIFlag != "--input-file" {
		t.Fatalf("commander params = %#v, want required input_file option", params)
	}
	if byName["format"].Required || byName["format"].CLIFlag != "--format" {
		t.Fatalf("commander params = %#v, want optional format option", params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("commander params = %#v, flag without value should be skipped", params)
	}
}

func TestExtractScriptParamsNodeYargsOptions(t *testing.T) {
	script := `
const argv = require('yargs')
  .option('inputFile', { demandOption: true, type: 'string' })
  .option('format', { type: 'string', default: 'pdf' })
  .option('verbose', { type: 'boolean' })
  .argv
`

	params := ExtractScriptParams(script, "javascript")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["input_file"].Required || byName["input_file"].CLIFlag != "--inputFile" {
		t.Fatalf("yargs params = %#v, want required inputFile option preserving CLI spelling", params)
	}
	if byName["format"].Required || byName["format"].CLIFlag != "--format" {
		t.Fatalf("yargs params = %#v, want optional format option", params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("yargs params = %#v, boolean option should be skipped", params)
	}
}

func TestExtractScriptParamsBashGetoptsSkipsNoValueFlags(t *testing.T) {
	script := `while getopts "i:o:vh" opt; do
case "$opt" in
  i) input="$OPTARG" ;;
  o) output="$OPTARG" ;;
  v) verbose=1 ;;
esac
done`

	params := ExtractScriptParams(script, "bash")
	byName := map[string]bool{}
	for _, param := range params {
		byName[param.Name] = param.Required && param.CLIFlag != ""
	}

	if !byName["i"] || !byName["o"] || byName["v"] || byName["h"] {
		t.Fatalf("bash getopts params = %#v, want only value-taking i/o flags", params)
	}
}

func TestExtractScriptParamsPowerShellParamBlock(t *testing.T) {
	script := `
param(
  [Parameter(Mandatory=$true)]
  [string]$InputPath,
  [string]$Format = "pdf",
  [switch]$Verbose
)
`

	params := ExtractScriptParams(script, "powershell")
	byName := map[string]corelib.NLSkillParam{}
	for _, param := range params {
		byName[param.Name] = param
	}

	if !byName["input_path"].Required || byName["input_path"].CLIFlag != "-InputPath" {
		t.Fatalf("PowerShell params = %#v, want required InputPath CLI param", params)
	}
	if byName["format"].Required || byName["format"].CLIFlag != "-Format" {
		t.Fatalf("PowerShell params = %#v, want optional Format CLI param", params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("PowerShell params = %#v, switch flag should not require a value", params)
	}
}

func TestExtractScriptParamsPowerShellArgs(t *testing.T) {
	params := ExtractScriptParams(`Write-Output "$($args[0]) -> $($args[1])"`, "pwsh")
	byName := map[string]bool{}
	for _, param := range params {
		byName[param.Name] = param.Required && param.CLIFlag == ""
	}
	if !byName["input"] || !byName["output"] {
		t.Fatalf("PowerShell args params = %#v, want positional input/output", params)
	}
}
