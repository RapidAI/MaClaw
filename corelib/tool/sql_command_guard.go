package tool

import (
	"regexp"
	"strings"
)

// Matches mysql/psql/sqlcmd (and siblings) as the command, not as a service
// name after systemctl/net. Same prefix soup as the SSH guard so sudo/env
// wrappers cannot smuggle a CLI through.
var rawDatabaseCLIPattern = regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:\./|[\w]:[\\/][^\s;&|()]+[\\/])?(?:mysql|mysql\.exe|mysqldump|mysqldump\.exe|mysqladmin|mysqladmin\.exe|mariadb|mariadb\.exe|psql|psql\.exe|pg_dump|pg_dump\.exe|sqlcmd|sqlcmd\.exe|osql|osql\.exe|sqlplus|sqlplus\.exe)(?:\s|$)`)

const rawDatabaseCLIRejection = "[system rejected] Database clients must not run through bash. Use the database or database_query tool with a configured profile; do not put passwords on the command line. If list_connections is empty, ask the user to add a data source in Settings."

// RejectShellDatabaseCLI rejects shell commands that try to bypass the
// builtin database tool with mysql/psql/sqlcmd (or dump/admin siblings).
func RejectShellDatabaseCLI(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if hasRawDatabaseCLI(command) || hasNestedRawDatabaseCLI(command) {
		return rawDatabaseCLIRejection, true
	}
	return "", false
}

func hasRawDatabaseCLI(command string) bool {
	return rawDatabaseCLIPattern.MatchString(command)
}

func hasNestedRawDatabaseCLI(command string) bool {
	tokens := shellLikeFields(command)
	for i, tok := range tokens {
		shell := shellLauncherName(tok)
		if shell == "" {
			continue
		}
		if nested := nestedShellCommand(tokens[i+1:], shell); nested != "" {
			if hasRawDatabaseCLI(nested) || hasRawDatabaseCLI("; "+nested) {
				return true
			}
		}
	}
	return false
}
