package main

import "testing"

func TestIsWorkflowProjectMutationPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// --- Should block: standard project source directories ---
		{"src/main.go", true},
		{"src/components/Button.tsx", true},
		{"app/routes/index.ts", true},
		{"cmd/server/main.go", true},
		{"internal/service/handler.go", true},
		{"pkg/utils/helper.py", true},
		{"web/index.html", true},
		{"frontend/src/App.jsx", true},
		{"backend/api/server.js", true},

		// --- Should block: Windows absolute paths to source dirs ---
		// Component matching: /src/, /cmd/, /internal/, /pkg/, /frontend/, /backend/
		// found anywhere in the path (safe dirs that won't produce false positives).
		{`D:\src\main.go`, true},
		{`C:\app\index.ts`, true},     // "app/" as prefix (relative-style after drive strip)
		{`D:\cmd\server\main.go`, true},
		{`D:\frontend\components\Button.tsx`, true},
		{`D:\workprj\src\main.go`, true},           // /src/ as component
		{`D:\workprj\myproject\cmd\server\main.go`, true}, // /cmd/ as component
		{`d:\workprj\internal\handler.go`, true},   // /internal/ as component
		{`C:\Users\dev\project\frontend\App.tsx`, true}, // /frontend/ as component
		{`C:\code\backend\api\server.js`, true},    // /backend/ as component

		// "app/" and "web/" only match as prefix (first component), NOT as substring,
		// because they produce false positives in absolute paths:
		// - "AppData" contains "app"
		// - "webapp" contains "web"
		{`C:\Users\dev\project\app\index.ts`, false}, // "app" not at root → not blocked
		{`D:\webapp\pages\index.tsx`, false},           // "webapp" != "web/"

		// --- Should NOT block: artifact generation scripts in non-source dirs ---
		{`D:\专利申请测试1\gen_pptx.js`, false},
		{`D:\专利申请测试1\ppt_content.json`, false},
		{`C:\Users\ma139\AppData\Local\Temp\generate.js`, false},
		{`C:\Users\ma139\.maclaw\workspace\gen_beijing_ppt.ps1`, false},
		{"gen_pptx.js", false},
		{"output/presentation.pptx", false},
		{"北京庆祝PPT.pptx", false},
		{"generate_slides.py", false},
		{"build_pptx.sh", false},
		{"run.js", false},
		{"main.py", false},

		// --- Should NOT block: temp/workspace paths ---
		{`C:\Users\ma139\AppData\Local\Temp\maclaw-skill-runs\pptx-generator\run.js`, false}, // AppData contains "app" but /appdata/ != /app/
		{"/tmp/craft_tool_output.py", false},
		{"node_modules/pptxgenjs/index.js", false},
		{`C:\Users\ma139\AppData\Local\Programs\node\run.js`, false}, // AppData safe

		// --- Should NOT block: data/config files in project root ---
		{"ppt_content.json", false},
		{"config.yaml", false},
		{"README.md", false},
		{".gitignore", false},

		// --- Edge cases ---
		{"", false},
		{"   ", false},
		{"./src/main.go", true}, // ./ prefix stripped, then src/ matches
		{`.\src\main.go`, true}, // backslash normalized, ./ stripped
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isWorkflowProjectMutationPath(tt.path)
			if got != tt.want {
				t.Errorf("isWorkflowProjectMutationPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestValidateWorkflowArtifactPhaseToolCall_AllowsArtifactScripts(t *testing.T) {
	// These represent the exact scenario that caused the 30-minute dead loop:
	// LLM writes a Node.js script to generate PPTX, in a non-source directory.
	tests := []struct {
		name string
		tool string
		args map[string]interface{}
		want string // "" means allowed, non-empty means blocked
	}{
		{
			name: "write_file to temp js script",
			tool: "write_file",
			args: map[string]interface{}{"path": `C:\Users\ma139\AppData\Local\Temp\generate.js`, "content": "const pptx = ..."},
			want: "",
		},
		{
			name: "write_file to project dir js script",
			tool: "write_file",
			args: map[string]interface{}{"path": `D:\专利申请测试1\gen_pptx.js`, "content": "..."},
			want: "",
		},
		{
			name: "write_file to pptx output",
			tool: "write_file",
			args: map[string]interface{}{"path": `D:\专利申请测试1\北京庆祝PPT.pptx`, "content": "binary..."},
			want: "",
		},
		{
			name: "write_file to src dir blocks",
			tool: "write_file",
			args: map[string]interface{}{"path": "src/components/Slide.tsx", "content": "..."},
			want: "artifact workflow phase cannot write into source/project paths (matched: src/). Use a temp or output directory instead.",
		},
		{
			name: "bash is allowed",
			tool: "bash",
			args: map[string]interface{}{"command": "node generate.js"},
			want: "",
		},
		{
			name: "list_directory is allowed",
			tool: "list_directory",
			args: map[string]interface{}{"path": "."},
			want: "",
		},
		{
			name: "edit_file is blocked",
			tool: "edit_file",
			args: map[string]interface{}{"path": "main.go"},
			want: "artifact workflow phase cannot run project mutation tools",
		},
		{
			name: "ssh is blocked",
			tool: "ssh",
			args: map[string]interface{}{"action": "exec"},
			want: "artifact workflow phase cannot run project mutation tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateWorkflowArtifactPhaseToolCall(tt.tool, tt.args)
			if got != tt.want {
				t.Errorf("validateWorkflowArtifactPhaseToolCall(%q, ...) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}
