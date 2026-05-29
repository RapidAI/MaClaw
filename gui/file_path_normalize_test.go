package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeFilePathForEvent_BasicRelative(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Absolute path within project, Windows backslashes.
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\gui\main.go`, `D:\workprj\aicoder`)
	if result != "gui/main.go" {
		t.Errorf("expected gui/main.go, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_NestedPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\corelib\agent\loop.go`, `D:\workprj\aicoder`)
	if result != "corelib/agent/loop.go" {
		t.Errorf("expected corelib/agent/loop.go, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_OutsideProject(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Path outside project root should return empty string.
	result := NormalizeFilePathForEvent(`C:\other\project\file.go`, `D:\workprj\aicoder`)
	if result != "" {
		t.Errorf("expected empty string for path outside project, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_DotDotSegments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Path with .. segments that resolves within project.
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\gui\..\corelib\agent\loop.go`, `D:\workprj\aicoder`)
	if result != "corelib/agent/loop.go" {
		t.Errorf("expected corelib/agent/loop.go, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_DotDotEscapesProject(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Path with .. that escapes project root.
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\..\other\file.go`, `D:\workprj\aicoder`)
	if result != "" {
		t.Errorf("expected empty string for path escaping project, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_EmptyInputs(t *testing.T) {
	if result := NormalizeFilePathForEvent("", "/project"); result != "" {
		t.Errorf("expected empty for empty filePath, got %q", result)
	}
	if result := NormalizeFilePathForEvent("/project/file.go", ""); result != "" {
		t.Errorf("expected empty for empty projectPath, got %q", result)
	}
	if result := NormalizeFilePathForEvent("", ""); result != "" {
		t.Errorf("expected empty for both empty, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_FileEqualsProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		result := NormalizeFilePathForEvent(`D:\workprj\aicoder`, `D:\workprj\aicoder`)
		if result != "." {
			t.Errorf("expected '.' when filePath equals projectPath, got %q", result)
		}
	} else {
		result := NormalizeFilePathForEvent("/home/user/project", "/home/user/project")
		if result != "." {
			t.Errorf("expected '.' when filePath equals projectPath, got %q", result)
		}
	}
}

func TestNormalizeFilePathForEvent_TrailingSlash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Trailing backslash on project path should not affect result.
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\gui\main.go`, `D:\workprj\aicoder\`)
	if result != "gui/main.go" {
		t.Errorf("expected gui/main.go with trailing slash on project, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_TrailingSlashUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	result := NormalizeFilePathForEvent("/home/user/project/src/main.go", "/home/user/project/")
	if result != "src/main.go" {
		t.Errorf("expected src/main.go with trailing slash on project, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_ForwardSlashOutput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Verify output always uses forward slashes.
	result := NormalizeFilePathForEvent(`D:\workprj\aicoder\src\components\App.tsx`, `D:\workprj\aicoder`)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if filepath.Separator == '\\' {
		// On Windows, ensure no backslashes in output.
		for _, c := range result {
			if c == '\\' {
				t.Errorf("output contains backslash: %q", result)
				break
			}
		}
	}
	if result != "src/components/App.tsx" {
		t.Errorf("expected src/components/App.tsx, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_CaseInsensitiveWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Windows paths are case-insensitive.
	result := NormalizeFilePathForEvent(`d:\WORKPRJ\AICODER\gui\main.go`, `D:\workprj\aicoder`)
	if result != "gui/main.go" {
		t.Errorf("expected gui/main.go, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	result := NormalizeFilePathForEvent("/home/user/project/src/main.go", "/home/user/project")
	if result != "src/main.go" {
		t.Errorf("expected src/main.go, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_UnixOutsideProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	result := NormalizeFilePathForEvent("/other/path/file.go", "/home/user/project")
	if result != "" {
		t.Errorf("expected empty string for path outside project, got %q", result)
	}
}

func TestNormalizeFilePathForEvent_UnixDotDotEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	result := NormalizeFilePathForEvent("/home/user/project/../other/file.go", "/home/user/project")
	if result != "" {
		t.Errorf("expected empty string for path escaping project via .., got %q", result)
	}
}

func TestNormalizeFilePathForEvent_WindowsForwardSlashInput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	// Forward slashes in input should be handled correctly on Windows.
	result := NormalizeFilePathForEvent("D:/workprj/aicoder/gui/main.go", "D:/workprj/aicoder")
	if result != "gui/main.go" {
		t.Errorf("expected gui/main.go for forward-slash input, got %q", result)
	}
}

// --- IsBinaryFile tests ---

func TestIsBinaryFile_NullByteAtStart(t *testing.T) {
	content := []byte{0x00, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
	if !IsBinaryFile(content) {
		t.Error("expected true for content with null byte at start")
	}
}

func TestIsBinaryFile_NullByteInMiddle(t *testing.T) {
	content := []byte("Hello\x00World")
	if !IsBinaryFile(content) {
		t.Error("expected true for content with null byte in middle")
	}
}

func TestIsBinaryFile_NullByteAt8191(t *testing.T) {
	// Null byte at position 8191 (within first 8192 bytes).
	content := make([]byte, 8192)
	for i := range content {
		content[i] = 'A'
	}
	content[8191] = 0x00
	if !IsBinaryFile(content) {
		t.Error("expected true for null byte at position 8191")
	}
}

func TestIsBinaryFile_NullByteAt8192(t *testing.T) {
	// Null byte at position 8192 (outside first 8192 bytes — should not be detected).
	content := make([]byte, 8193)
	for i := range content {
		content[i] = 'A'
	}
	content[8192] = 0x00
	if IsBinaryFile(content) {
		t.Error("expected false for null byte at position 8192 (beyond check range)")
	}
}

func TestIsBinaryFile_PureText(t *testing.T) {
	content := []byte("Hello, World! This is a normal text file.\nWith multiple lines.\n")
	if IsBinaryFile(content) {
		t.Error("expected false for pure text content")
	}
}

func TestIsBinaryFile_UTF8Text(t *testing.T) {
	content := []byte("你好世界！这是一个UTF-8文本文件。\n包含中文字符。\n")
	if IsBinaryFile(content) {
		t.Error("expected false for UTF-8 text content")
	}
}

func TestIsBinaryFile_EmptyContent(t *testing.T) {
	if IsBinaryFile([]byte{}) {
		t.Error("expected false for empty content")
	}
}

func TestIsBinaryFile_LargeTextFile(t *testing.T) {
	// Large file with no null bytes.
	content := make([]byte, 16384)
	for i := range content {
		content[i] = byte('A' + (i % 26))
	}
	if IsBinaryFile(content) {
		t.Error("expected false for large text file without null bytes")
	}
}
