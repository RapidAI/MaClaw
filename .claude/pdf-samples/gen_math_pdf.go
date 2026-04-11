package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

func main() {
	content := `# Markdown PDF 渲染样例

## 有序列表
1. 第一项
2. 第二项
3. 第三项

## 行内公式
- 欧拉恒等式：$e^{i\pi} + 1 = 0$
- 化学式：$H_2O$
- 极限：$x \to \infty$

## 分段函数
$$\begin{cases}x^2 & x > 0\\0 & x = 0\\-x & x < 0\end{cases}$$

## 对齐公式
$$\begin{aligned}f(x) &= x^2 + 1\\g(x) &= x + 1\end{aligned}$$

## 行列式
$$\begin{vmatrix}a & b\\c & d\end{vmatrix}$$
`

	path, err := swarm.GenerateToFile(content, "markdown_pdf_render_sample", "requirements", `D:/workprj/aicoder/.claude/pdf-samples/markdown_pdf_render_sample.pdf`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}
