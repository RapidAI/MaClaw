# OmniParser V2 集成方案——从机制角度

## 一、问题本质

maclaw 的桌面 GUI 观测有四个手段，各有一个致命盲区：

| 手段 | 能力 | 盲区 |
|------|------|------|
| `accessibility.Bridge` | 结构化元素树 + 精确坐标 + 语义（role/name/value） | **依赖程序暴露 UIA 信息**。Electron 应用、游戏、自绘控件、远程桌面窗口不暴露或暴露不完整 |
| `RapidOCR` | 文本识别 + 精确坐标 | **只识别文本，不识别 UI 语义**。看到"确定"两个字，不知道它是按钮还是标签 |
| `LLM Vision` | 全能理解 | **慢（2-10s）+ 不返回精确像素坐标**。无法用于实时元素定位 |
| `ImageMatcher` | 像素级模板匹配 | **需要预录制参考图片**。UI 变化（主题/分辨率/语言）后失效 |

这四个盲区叠加的结果：当 accessibility 不可用时（大量真实场景），Agent 只能截屏让 LLM 看图说话，每次观测消耗一次完整 LLM 推理（~130K input token），且无法精确点击目标元素。

**OmniParser V2 填补的空白**：纯视觉的 UI 语义理解——截图 → 结构化元素列表（bounding box + 功能描述 + 可交互性），不依赖 accessibility API，不需要 LLM 推理，延迟 <1s，返回精确像素坐标。

## 二、机制性分析：OmniParser 应该接在哪里

### 不应该做的事（workaround）

1. **不应该把 OmniParser 做成一个独立的工具**（如 `gui_omniparse`）。这会创建第五个独立的观测手段，与现有四个互不通信。Agent 需要自己判断"这次该用 accessibility 还是 OCR 还是 OmniParser"——这个判断本身就是一个 LLM 推理开销。

2. **不应该把 OmniParser 做成 OCRProvider 的替代品**。OmniParser 的输出不是 OCR 文本——它是结构化的 UI 元素列表（类型 + 描述 + 坐标 + 可交互性）。把它塞进 `OCRProvider` 接口会丢失语义信息。

3. **不应该只在 `gui_observe` 工具中调用 OmniParser**。`gui_observe` 是 Agent 主动调用的工具，但 OmniParser 的价值在于增强整个观测栈——包括 `GUIStateObserver.Verify()`、`ElementLocator.Locate()`、`GUITaskSupervisor` 的重试决策。

### 应该做的事（机制性修复）

OmniParser 应该作为 `ElementLocator` 的**第四层定位策略**和 `GUIStateObserver` 的**第二观测源**，与现有三层策略统一编排。

当前 `ElementLocator.Locate()` 的三层策略：
```
Tier 1: Accessibility Bridge → FindElement(role, name)
Tier 2: Image Matcher → FindByImage(snapshotRef)
Tier 3: Coordinate fallback → 使用录制时的坐标
```

加入 OmniParser 后：
```
Tier 1: Accessibility Bridge → FindElement(role, name)
Tier 2: OmniParser → ParseScreen() → 按功能描述匹配目标元素
Tier 3: Image Matcher → FindByImage(snapshotRef)
Tier 4: Coordinate fallback → 使用录制时的坐标
```

**为什么 OmniParser 排在 Tier 2（Accessibility 之后、Image Matcher 之前）**：
- Accessibility 是确定性的结构化数据，零推理开销，有就用——排第一
- OmniParser 是视觉模型推理（~0.8s），返回结构化数据+精确坐标——排第二
- Image Matcher 是像素级模板匹配，需要预录制参考图——排第三
- 坐标 fallback 是最后手段——排第四

## 三、统一接口设计

### 核心洞察：OmniParser 的输出和 Accessibility Bridge 的输出在语义上是同构的

| 字段 | Accessibility Element | OmniParser Element |
|------|----------------------|-------------------|
| 类型 | `Role`（Button/Edit/MenuItem...） | `type`（button/input/icon/text...） |
| 名称 | `Name`（"确定"/"用户名"） | `caption`（"confirm button"/"search icon"） |
| 坐标 | `Bounds`（x, y, width, height） | `bbox`（x, y, width, height） |
| 值 | `Value`（输入框内容） | 无（OmniParser 不读取控件值） |
| 可交互 | 隐含（role 决定） | `interactable`（显式标注） |

两者都是"截图上有哪些 UI 元素、在哪里、是什么"的结构化回答。差异只在数据来源（平台 API vs 视觉模型）和精度特征（Accessibility 有 Value 但依赖程序支持；OmniParser 无 Value 但不依赖程序支持）。

### 新增 `UIElement` 统一类型（`corelib/taskengine/types.go`）

```go
// UIElement is a platform-agnostic representation of a UI element detected
// on screen. Both accessibility.Bridge and OmniParser produce UIElements.
type UIElement struct {
    Type        string  `json:"type"`         // button, edit, text, icon, menu, checkbox, etc.
    Name        string  `json:"name"`         // human-readable label or functional description
    Value       string  `json:"value"`        // current value (accessibility only; empty for vision)
    BBox        [4]int  `json:"bbox"`         // x, y, width, height in screen coordinates
    Interactable bool   `json:"interactable"` // whether the element can be clicked/typed into
    Confidence  float64 `json:"confidence"`   // 1.0 for accessibility, model confidence for vision
    Source      string  `json:"source"`       // "accessibility", "omniparser", "ocr"
}
```

### 新增 `ScreenParser` 接口（`corelib/taskengine/interfaces.go`）

```go
// ScreenParser converts a screenshot into structured UI elements.
// Implementations: OmniParser sidecar, accessibility bridge adapter, etc.
type ScreenParser interface {
    // Parse takes a base64-encoded PNG screenshot and returns detected UI elements.
    Parse(pngBase64 string) ([]UIElement, error)
    // IsAvailable returns true if the parser is ready to use.
    IsAvailable() bool
}
```

### 三个 `ScreenParser` 实现

1. **`AccessibilityScreenParser`**：包装 `accessibility.Bridge`，将 `EnumElements` 结果转换为 `[]UIElement`。不需要截图输入（忽略 pngBase64 参数），直接查询平台 API。

2. **`OmniParserSidecar`**：Python sidecar 进程（复用 `RapidOCRSidecar` 的 stdin/stdout JSON 协议），加载 OmniParser V2 模型，接收截图返回 `[]UIElement`。

3. **`OCRScreenParser`**：包装现有 `OCRProvider`，将 OCR 结果转换为 `[]UIElement`（type="text"，interactable=false）。

### `CompositeScreenParser`：统一编排

```go
// CompositeScreenParser tries multiple parsers and merges results.
// Unlike CompositeOCRProvider (first-success), this merges all available
// results because different parsers detect different element types.
type CompositeScreenParser struct {
    parsers []ScreenParser
}

func (c *CompositeScreenParser) Parse(pngBase64 string) ([]UIElement, error) {
    var all []UIElement
    for _, p := range c.parsers {
        if p == nil || !p.IsAvailable() {
            continue
        }
        elements, err := p.Parse(pngBase64)
        if err != nil {
            continue // try next
        }
        all = append(all, elements...)
    }
    return deduplicateByBBox(all), nil
}
```

**关键设计决策：merge 而非 first-success。** `CompositeOCRProvider` 用 first-success 是因为所有 OCR 后端做同一件事（识别文本）。但 `CompositeScreenParser` 的后端做不同的事——Accessibility 返回控件树（有 Value），OmniParser 返回视觉元素（有 Interactable），OCR 返回纯文本。Merge 后的结果比任何单一来源都更完整。

`deduplicateByBBox`：当两个来源检测到同一位置的元素时（BBox 重叠 >80%），保留 confidence 更高的那个，但合并两者的信息（如 Accessibility 的 Value + OmniParser 的 Interactable）。

## 四、接入点

### 接入点 1：`ElementLocator.Locate()` — 元素定位

```go
func (l *ElementLocator) Locate(step GUIRecordedStep) (*LocateResult, error) {
    // Tier 1: Accessibility (unchanged)
    ...

    // Tier 2: ScreenParser (NEW — replaces nothing, inserts between accessibility and image)
    if l.screenParser != nil && l.screenParser.IsAvailable() {
        screenshot, _ := l.screenshotFn()
        if screenshot != "" {
            elements, _ := l.screenParser.Parse(screenshot)
            if match := findBestMatch(elements, step); match != nil {
                return &LocateResult{
                    Strategy:   StrategyVision,
                    X:          match.BBox[0] + match.BBox[2]/2,
                    Y:          match.BBox[1] + match.BBox[3]/2,
                    Confidence: match.Confidence,
                }, nil
            }
        }
    }

    // Tier 3: Image Matcher (unchanged)
    ...
    // Tier 4: Coordinate fallback (unchanged)
    ...
}
```

`findBestMatch` 用录制步骤的 `AccessibilityID`（role + name）和 `Text` 与 OmniParser 返回的元素做语义匹配（子串匹配 + 类型映射）。

### 接入点 2：`GUIStateObserver.Snapshot()` — 状态快照

```go
func (o *GUIStateObserver) Snapshot() (*taskengine.StateSnapshot, error) {
    snap := &taskengine.StateSnapshot{}

    // Screenshot
    if img, _ := o.screenshotFn(); img != "" {
        snap.ScreenshotB64 = img

        // ScreenParser: structured UI elements (NEW)
        if o.screenParser != nil && o.screenParser.IsAvailable() {
            elements, _ := o.screenParser.Parse(img)
            snap.UIElements = elements  // 新增字段
        }
    }

    // OCR (unchanged, but now redundant if ScreenParser includes text elements)
    ...
}
```

### 接入点 3：`GUIStateObserver.Verify()` — 验证增强

OmniParser 的结构化输出让 `element_exists` 和 `text_contains` 验证不再完全依赖 accessibility bridge 和 OCR：

```go
func (o *GUIStateObserver) checkElementExists(c taskengine.CriterionSpec) taskengine.CriterionResult {
    // Tier 1: Accessibility bridge (unchanged)
    if o.bridge != nil {
        el, _ := o.bridge.FindElement(...)
        if el != nil { return pass }
    }

    // Tier 2: ScreenParser (NEW — fallback when accessibility fails)
    if o.screenParser != nil && o.screenParser.IsAvailable() {
        img, _ := o.screenshotFn()
        elements, _ := o.screenParser.Parse(img)
        for _, el := range elements {
            if matchesCriterion(el, c) {
                return pass
            }
        }
    }

    return fail
}
```

### 接入点 4：`gui_observe` 工具 — Agent 观测增强

当 accessibility bridge 返回空（程序不暴露 UIA 信息）时，`gui_observe` 自动 fallback 到 OmniParser：

```
gui_observe(window="Photoshop")
→ Accessibility: 无元素（Photoshop 不暴露 UIA）
→ OmniParser fallback:
  窗口 "Photoshop" 元素（视觉检测）:
    [button] "New File" (120x40 at 10,50) interactable
    [icon] "Brush Tool" (32x32 at 5,200) interactable
    [text] "Layers" (60x20 at 800,100)
    [input] "Opacity: 100%" (80x25 at 400,50) interactable
    ...
```

## 五、OmniParser Sidecar 实现

复用 `RapidOCRSidecar` 的模式：

### Python sidecar 脚本

```python
#!/usr/bin/env python3
"""OmniParser V2 sidecar — stdin/stdout JSON protocol."""
import sys, json, base64
from PIL import Image
import io

def main():
    # Load models once at startup
    from ultralytics import YOLO
    from transformers import AutoProcessor, AutoModelForCausalLM
    import torch

    yolo = YOLO('weights/icon_detect/model.pt')
    processor = AutoProcessor.from_pretrained("microsoft/Florence-2-base", trust_remote_code=True)
    caption_model = AutoModelForCausalLM.from_pretrained(
        "weights/icon_caption_florence", torch_dtype=torch.float16, trust_remote_code=True
    )
    if torch.cuda.is_available():
        yolo = yolo.to('cuda')
        caption_model = caption_model.to('cuda')

    for line in sys.stdin:
        req = json.loads(line.strip())
        if req["method"] == "parse":
            img_bytes = base64.b64decode(req["image_base64"])
            img = Image.open(io.BytesIO(img_bytes))
            # YOLO detection
            detections = yolo(img)
            elements = []
            for det in detections[0].boxes:
                bbox = det.xyxy[0].tolist()  # x1, y1, x2, y2
                x, y = int(bbox[0]), int(bbox[1])
                w, h = int(bbox[2] - bbox[0]), int(bbox[3] - bbox[1])
                conf = float(det.conf[0])
                # Crop and caption
                crop = img.crop((x, y, x+w, y+h))
                caption = generate_caption(processor, caption_model, crop)
                elements.append({
                    "type": classify_type(caption),
                    "name": caption,
                    "bbox": [x, y, w, h],
                    "interactable": conf > 0.5,
                    "confidence": round(conf, 3)
                })
            print(json.dumps({"elements": elements}), flush=True)
        elif req["method"] == "ping":
            print(json.dumps({"status": "ok"}), flush=True)

if __name__ == "__main__":
    main()
```

### Go 侧 `OmniParserSidecar`

与 `RapidOCRSidecar` 结构相同：
- 首次调用时自动安装（`pip install ultralytics transformers` + 下载模型权重）
- stdin/stdout JSON-RPC 通信
- 5 分钟空闲自动关闭（释放 GPU 显存）
- 崩溃自动重启

### 无 GPU fallback

OmniParser V2 需要 GPU（YOLO + Florence-2）。无 GPU 时：
- `IsAvailable()` 返回 false
- `CompositeScreenParser` 跳过 OmniParser，使用 Accessibility + OCR
- 行为与当前完全一致——OmniParser 是增强，不是依赖

## 六、`StateSnapshot` 扩展

```go
type StateSnapshot struct {
    ScreenshotB64  string            `json:"screenshot_b64,omitempty"`
    OCRText        string            `json:"ocr_text,omitempty"`
    UIElements     []UIElement       `json:"ui_elements,omitempty"`  // NEW
    WindowTitle    string            `json:"window_title,omitempty"`
    URL            string            `json:"url,omitempty"`
    Title          string            `json:"title,omitempty"`
    FocusedElement map[string]string `json:"focused_element,omitempty"`
    Extra          map[string]string `json:"extra,omitempty"`
}
```

`UIElements` 字段让 `RetryClassifier` 在重试决策时有结构化的 UI 上下文——不再只有 OCR 文本和截图，而是知道屏幕上有哪些按钮、输入框、菜单。

## 七、依赖关系

```
corelib/taskengine/
    types.go        ← UIElement, ScreenParser 接口
    interfaces.go   ← ScreenParser 接口

corelib/guiautomation/
    screen_parser_omni.go      ← OmniParserSidecar (实现 ScreenParser)
    screen_parser_a11y.go      ← AccessibilityScreenParser (实现 ScreenParser)
    screen_parser_ocr.go       ← OCRScreenParser (实现 ScreenParser)
    screen_parser_composite.go ← CompositeScreenParser
    locator.go                 ← Locate() 新增 Tier 2: ScreenParser
    state_observer.go          ← Snapshot() + Verify() 接入 ScreenParser

gui/
    tools_gui_automation.go    ← gui_observe fallback 到 ScreenParser
```

OmniParser 不引入任何新的包间依赖。`ScreenParser` 接口定义在 `taskengine`（与 `StateObserver`/`StepExecutor` 同级），实现在 `guiautomation`。

## 八、实施顺序

1. **`taskengine/types.go`**：新增 `UIElement` 类型 + `StateSnapshot.UIElements` 字段
2. **`taskengine/interfaces.go`**：新增 `ScreenParser` 接口
3. **`guiautomation/screen_parser_a11y.go`**：Accessibility → ScreenParser 适配器（纯类型转换，零新逻辑）
4. **`guiautomation/screen_parser_ocr.go`**：OCR → ScreenParser 适配器
5. **`guiautomation/screen_parser_composite.go`**：CompositeScreenParser + deduplicateByBBox
6. **`guiautomation/locator.go`**：Locate() 新增 Tier 2
7. **`guiautomation/state_observer.go`**：Snapshot() + Verify() 接入
8. **`gui/tools_gui_automation.go`**：gui_observe fallback
9. **`guiautomation/screen_parser_omni.go`**：OmniParser sidecar（最后做——依赖 GPU 环境测试）

步骤 1-8 不依赖 OmniParser 模型，可以用 Accessibility + OCR 的 ScreenParser 实现完整测试。步骤 9 是 OmniParser 特有的 sidecar，独立开发和测试。

## 九、机制性保证

1. **OmniParser 是增强不是依赖**：`CompositeScreenParser` 在 OmniParser 不可用时自动降级到 Accessibility + OCR。所有现有功能不受影响。

2. **新增观测源自动受益于整个栈**：OmniParser 的结果通过 `ScreenParser` 接口流入 `ElementLocator`、`GUIStateObserver`、`gui_observe`、`gui_verify`、`RetryClassifier`——不需要在每个消费方单独接入。

3. **未来新增视觉模型只需实现 `ScreenParser`**：如果将来有比 OmniParser 更好的模型（如 GPT-4o 的 structured output mode），只需新增一个 `ScreenParser` 实现，插入 `CompositeScreenParser` 的 parsers 列表。

4. **Accessibility 和 OmniParser 的结果互补而非替代**：`deduplicateByBBox` 合并两者信息——Accessibility 提供 Value（输入框内容），OmniParser 提供 Interactable 标注和视觉元素。合并后的 `UIElement` 比任何单一来源都更完整。
