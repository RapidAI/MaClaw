package v2

import "testing"

func TestParseTaskList(t *testing.T) {
	input := `# 任务拆分

### T1: 项目初始化
- **描述**：创建项目目录和 CMakeLists.txt
- **涉及文件**：CMakeLists.txt, main.cpp
- **依赖**：无
- **优先级**：P0
- **工作量**：30分钟

### T2: 游戏核心逻辑
- **描述**：实现蛇移动、食物生成、碰撞检测
- **涉及文件**：game.cpp, game.h
- **依赖**：依赖 T1
- **优先级**：P0
- **工作量**：2小时

### T3: 渲染模块
- **描述**：Windows 控制台渲染
- **涉及文件**：renderer.cpp, renderer.h
- **依赖**：依赖 T1, T2
- **优先级**：P1
- **工作量**：1小时
`
	tasks := ParseTaskList(input)
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}

	// T1
	if tasks[0].Index != 1 {
		t.Errorf("T1 index = %d", tasks[0].Index)
	}
	if tasks[0].Title != "项目初始化" {
		t.Errorf("T1 title = %q", tasks[0].Title)
	}
	if tasks[0].Description != "创建项目目录和 CMakeLists.txt" {
		t.Errorf("T1 description = %q", tasks[0].Description)
	}
	if len(tasks[0].Files) != 2 {
		t.Errorf("T1 files = %v", tasks[0].Files)
	}
	if len(tasks[0].DependsOn) != 0 {
		t.Errorf("T1 depends = %v", tasks[0].DependsOn)
	}

	// T2
	if tasks[1].Index != 2 {
		t.Errorf("T2 index = %d", tasks[1].Index)
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != 1 {
		t.Errorf("T2 depends = %v", tasks[1].DependsOn)
	}

	// T3
	if len(tasks[2].DependsOn) != 2 {
		t.Errorf("T3 depends = %v, want [1,2]", tasks[2].DependsOn)
	}
}

func TestParseTaskList_Empty(t *testing.T) {
	tasks := ParseTaskList("no tasks here")
	if len(tasks) != 0 {
		t.Fatalf("got %d tasks, want 0", len(tasks))
	}
}

func TestParseTaskList_BareFormat(t *testing.T) {
	// Format seen in real LLM output: "T0: CMakeLists.txt"
	input := `T0: CMakeLists.txt
T1: Config.h + Types.h
T2: Snake.h + Snake.cpp
T3: Food.h + Food.cpp
T4: Renderer.h + Renderer.cpp
T5: Input.h + Input.cpp
T6: Audio.h + Audio.cpp
T7: Game.h + Game.cpp
T8: main.cpp
`
	tasks := ParseTaskList(input)
	if len(tasks) != 9 {
		t.Fatalf("got %d tasks, want 9", len(tasks))
	}
	if tasks[0].Index != 0 || tasks[0].Title != "CMakeLists.txt" {
		t.Errorf("T0 = %+v", tasks[0])
	}
	if tasks[8].Index != 8 || tasks[8].Title != "main.cpp" {
		t.Errorf("T8 = %+v", tasks[8])
	}
}

func TestParseTaskList_BoldFormat(t *testing.T) {
	input := `**T1: 项目初始化**
- 描述：创建项目

**T2: 核心逻辑**
- 描述：写游戏
`
	tasks := ParseTaskList(input)
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].Title != "项目初始化" {
		t.Errorf("T1 title = %q", tasks[0].Title)
	}
}
