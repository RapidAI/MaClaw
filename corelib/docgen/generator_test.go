package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkdownToHTML_Headings(t *testing.T) {
	md := "# 大标题\n## 二级标题\n### 三级标题\n#### 四级标题\n##### 五级标题"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<b>大标题</b>") {
		t.Error("should contain h1 text")
	}
	if !strings.Contains(html, "<b>二级标题</b>") {
		t.Error("should contain h2 text")
	}
	if !strings.Contains(html, "<b>三级标题</b>") {
		t.Error("should contain h3 text")
	}
	if !strings.Contains(html, "<b>四级标题</b>") {
		t.Error("should contain h4 text")
	}
	if !strings.Contains(html, "<b>五级标题</b>") {
		t.Error("should contain h5 text")
	}
	if !strings.Contains(html, "15pt") {
		t.Error("h1 should use 15pt font")
	}
	if !strings.Contains(html, "13pt") {
		t.Error("h2 should use 13pt font")
	}
	if !strings.Contains(html, "11pt") {
		t.Error("h3 should use 11pt font")
	}
	if !strings.Contains(html, "9.5pt") {
		t.Error("h4 should use 9.5pt font")
	}
	if !strings.Contains(html, "8.5pt") {
		t.Error("h5 should use 8.5pt font")
	}
}

func TestStripDuplicateLeadingHeading_RemovesMatchingH1(t *testing.T) {
	spec := Spec{
		Title:       "Hugging Face Daily Papers 综述与评论",
		ProjectName: "Hugging Face Daily Papers 综述与评论",
		Content:     "# Hugging Face Daily Papers 综述与评论\n\n日期：2025 年 4 月 3 日\n\n## 小节",
	}
	got := stripDuplicateLeadingHeading(spec)
	if strings.Contains(got, "# Hugging Face Daily Papers 综述与评论") {
		t.Fatalf("expected duplicate leading H1 to be removed, got %q", got)
	}
	if !strings.Contains(got, "日期：2025 年 4 月 3 日") {
		t.Fatalf("expected body content to remain, got %q", got)
	}
	if !strings.Contains(got, "## 小节") {
		t.Fatalf("expected following headings to remain, got %q", got)
	}
}

func TestStripDuplicateLeadingHeading_KeepsDifferentH1(t *testing.T) {
	spec := Spec{
		ProjectName: "Hugging Face Daily Papers 综述与评论",
		Content:     "# 今日重点论文\n\n正文",
	}
	got := stripDuplicateLeadingHeading(spec)
	if got != spec.Content {
		t.Fatalf("expected non-matching H1 to stay unchanged, got %q", got)
	}
}

func TestStripDuplicateLeadingHeading_IgnoresNonLeadingH1(t *testing.T) {
	spec := Spec{
		ProjectName: "Hugging Face Daily Papers 综述与评论",
		Content:     "> 摘要\n\n# Hugging Face Daily Papers 综述与评论\n\n正文",
	}
	got := stripDuplicateLeadingHeading(spec)
	if got != spec.Content {
		t.Fatalf("expected H1 after other content to stay unchanged, got %q", got)
	}
}
func TestNormalizeHeadingText(t *testing.T) {
	if got := normalizeHeadingText("  标题   ### "); got != "标题" {
		t.Fatalf("normalizeHeadingText returned %q", got)
	}
	if got := normalizeHeadingText("A   B"); got != "A B" {
		t.Fatalf("normalizeHeadingText collapsed spaces incorrectly: %q", got)
	}
}

func TestMarkdownToHTML_Lists(t *testing.T) {
	md := "- 第一项\n- 第二项\n* 第三项"
	html := markdownToHTML(md)
	liCount := strings.Count(html, "<li>")
	if !strings.Contains(html, "<ul>") {
		t.Error("should contain <ul>")
	}
	if !strings.Contains(html, "<li>第一项</li>") {
		t.Error("should contain list items")
	}
	if liCount != 3 {
		t.Errorf("expected 3 list items, got %d", liCount)
	}
}

func TestMarkdownToHTML_NumberedList(t *testing.T) {
	md := "1. 步骤一\n2. 步骤二"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<ol>") {
		t.Error("should contain <ol>")
	}
	if !strings.Contains(html, "<li>步骤一</li>") {
		t.Error("should parse numbered list")
	}
	if strings.Contains(html, "<ul>") {
		t.Error("numbered list should not render as <ul>")
	}
	if strings.Count(html, "<ol>") != 1 {
		t.Errorf("expected one ordered list, got %d", strings.Count(html, "<ol>"))
	}
}

func TestMarkdownToHTML_NumberedListClosesBeforeParagraph(t *testing.T) {
	md := "1. 第一步\n2. 第二步\n说明段落"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<ol><li>第一步</li><li>第二步</li></ol><p>说明段落</p>") {
		t.Errorf("unexpected ordered list html: %s", html)
	}
}

func TestMarkdownToHTML_InlineMath(t *testing.T) {
	math := transformMathText(`\alpha + \beta \le \gamma`)
	if math != `α + β ≤ γ` {
		t.Fatalf("unexpected transformMathText output: %s", math)
	}
	if transformed := transformMathText(`x^2 + H_2O`); transformed != `x<sup>2</sup> + H<sub>2</sub>O` {
		t.Fatalf("unexpected inline power/subscript transform: %s", transformed)
	}

	md := `公式：$\alpha + \beta \le \gamma$，以及 $x^2 + H_2O$`
	html := markdownToHTML(md)
	if strings.Contains(html, "$\\alpha") {
		t.Error("raw inline math should not remain")
	}
	if !strings.Contains(html, "α + β ≤ γ") {
		t.Errorf("expected unicode math symbols, got %s", html)
	}
	if !strings.Contains(html, "x<sup>2</sup>") {
		t.Errorf("expected superscript conversion, got %s", html)
	}
	if !strings.Contains(html, "H<sub>2</sub>O") {
		t.Errorf("expected subscript conversion, got %s", html)
	}
}

func TestMarkdownToHTML_DisplayMath(t *testing.T) {
	md := "$$\\sum_{i=1}^{n} x_i$$"
	html := markdownToHTML(md)
	if strings.Contains(html, "$$") {
		t.Error("raw display math delimiters should not remain")
	}
	if !strings.Contains(html, `<p style="text-align:center">∑<sub>i=1</sub><sup>n</sup> x<sub>i</sub></p>`) {
		t.Errorf("unexpected display math html: %s", html)
	}
}

func TestMarkdownToHTML_MultiLineDisplayMath(t *testing.T) {
	md := "$$\\int_0^1 x^2 dx\n+ \\alpha\n$$"
	html := markdownToHTML(md)
	if strings.Contains(html, "$$") {
		t.Error("raw multi-line display math delimiters should not remain")
	}
	if !strings.Contains(html, `<p style="text-align:center">∫<sub>0</sub><sup>1</sup> x<sup>2</sup> dx + α</p>`) {
		t.Errorf("unexpected multi-line display math html: %s", html)
	}
}

func TestMarkdownToHTML_AdvancedMathReadableFallback(t *testing.T) {
	math := transformMathText(`\frac{a+b}{c} + \sqrt{x^2+1}`)
	if !strings.Contains(math, `<span style="white-space:nowrap">(a+b)/(c)</span>`) {
		t.Fatalf("expected fraction fallback, got %s", math)
	}
	if !strings.Contains(math, `√x<sup>2</sup>+1`) {
		t.Fatalf("expected sqrt fallback, got %s", math)
	}

	matrix := transformMathText(`\begin{bmatrix}a & b\\c & d\end{bmatrix}`)
	if matrix != `[[a, b]; [c, d]]` {
		t.Fatalf("unexpected matrix fallback: %s", matrix)
	}

	plainMatrix := transformMathText(`\begin{matrix}a & b\\c & d\end{matrix}`)
	if plainMatrix != `matrix[[a, b]; [c, d]]` {
		t.Fatalf("unexpected matrix fallback: %s", plainMatrix)
	}

	smallMatrix := transformMathText(`\begin{smallmatrix}a & b\\c & d\end{smallmatrix}`)
	if smallMatrix != `smallmatrix[[a, b]; [c, d]]` {
		t.Fatalf("unexpected smallmatrix fallback: %s", smallMatrix)
	}

	parenMatrix := transformMathText(`\begin{pmatrix}a & b\\c & d\end{pmatrix}`)
	if parenMatrix != `((a, b); (c, d))` {
		t.Fatalf("unexpected pmatrix fallback: %s", parenMatrix)
	}

	braceMatrix := transformMathText(`\begin{Bmatrix}a & b\\c & d\end{Bmatrix}`)
	if braceMatrix != `brace[[a, b]; [c, d]]` {
		t.Fatalf("unexpected Bmatrix fallback: %s", braceMatrix)
	}

	determinant := transformMathText(`\begin{vmatrix}a & b\\c & d\end{vmatrix}`)
	if determinant != `det[[a, b]; [c, d]]` {
		t.Fatalf("unexpected determinant fallback: %s", determinant)
	}

	norm := transformMathText(`\begin{Vmatrix}a & b\\c & d\end{Vmatrix}`)
	if norm != `norm[[a, b]; [c, d]]` {
		t.Fatalf("unexpected norm fallback: %s", norm)
	}

	cases := transformMathText(`\begin{cases}x^2 & x > 0\\0 & x = 0\\-x & x < 0\end{cases}`)
	if cases != `cases(x<sup>2</sup>, x &gt; 0; 0, x = 0; -x, x &lt; 0)` {
		t.Fatalf("unexpected cases fallback: %s", cases)
	}

	aligned := transformMathText(`\begin{aligned}f(x) &= x^2 + 1\\g(x) &= x + 1\end{aligned}`)
	if aligned != `f(x) = x<sup>2</sup> + 1 ; g(x) = x + 1` {
		t.Fatalf("unexpected aligned fallback: %s", aligned)
	}

	eqnarray := transformMathText(`\begin{eqnarray}f(x) &=& x^2 + 1\\g(x) &=& x + 1\end{eqnarray}`)
	if eqnarray != `f(x) = x<sup>2</sup> + 1 ; g(x) = x + 1` {
		t.Fatalf("unexpected eqnarray fallback: %s", eqnarray)
	}

	array := transformMathText(`\begin{array}{cc}x^2 & y^2\\1 & 2\end{array}`)
	if array != `array[x<sup>2</sup> | y<sup>2</sup>; 1 | 2]` {
		t.Fatalf("unexpected array fallback: %s", array)
	}

	binom := transformMathText(`\binom{n}{k}`)
	if binom != `binom(n, k)` {
		t.Fatalf("unexpected binom fallback: %s", binom)
	}

	decorated := transformMathText(`\overline{AB} + \underline{CD} + \hat{x} + \bar{y} + \vec{v}`)
	if decorated != `overline(AB) + underline(CD) + hat(x) + bar(y) + vec(v)` {
		t.Fatalf("unexpected decorator fallback: %s", decorated)
	}

	moreDecorated := transformMathText(`\tilde{x} + \dot{y} + \ddot{z} + \cancel{a+b} + \boxed{n}`)
	if moreDecorated != `tilde(x) + dot(y) + ddot(z) + cancel(a+b) + boxed(n)` {
		t.Fatalf("unexpected extended decorator fallback: %s", moreDecorated)
	}

	positioned := transformMathText(`\overset{def}{=} + \underset{n\to\infty}{lim} + \operatorname{sin}(x)`)
	if positioned != `overset(def, =) + underset(n→∞, lim) + sin(x)` {
		t.Fatalf("unexpected positioned/operator fallback: %s", positioned)
	}

	operators := transformMathText(`\sum_{i=1}^{n} x_i + \prod_{k=1}^{m} y_k + \int_{0}^{1} t^2 dt + \lim_{n \to \infty} a_n`)
	if operators != `∑<sub>i=1</sub><sup>n</sup> x<sub>i</sub> + ∏<sub>k=1</sub><sup>m</sup> y<sub>k</sub> + ∫<sub>0</sub><sup>1</sup> t<sup>2</sup> dt + lim<sub>n → ∞</sub> a<sub>n</sub>` {
		t.Fatalf("unexpected operator fallback: %s", operators)
	}

	nestedFrac := transformMathText(`\frac{1}{\frac{a}{b}}`)
	if nestedFrac != `<span style="white-space:nowrap">(1)/(<span style="white-space:nowrap">(a)/(b)</span>)</span>` {
		t.Fatalf("unexpected nested fraction fallback: %s", nestedFrac)
	}

	functions := transformMathText(`\sin(x) + \cos(y) + \tan(z) + \log n + \ln m + \exp(t)`)
	if functions != `sin(x) + cos(y) + tan(z) + log n + ln m + exp(t)` {
		t.Fatalf("unexpected function fallback: %s", functions)
	}

	optimizers := transformMathText(`\min_x f(x) + \max_y g(y) + \argmin_z h(z) + \argmax_w q(w)`)
	if optimizers != `min<sub>x</sub> f(x) + max<sub>y</sub> g(y) + argmin<sub>z</sub> h(z) + argmax<sub>w</sub> q(w)` {
		t.Fatalf("unexpected optimizer fallback: %s", optimizers)
	}

	styles := transformMathText(`\mathbb{R} + \mathbf{x} + \mathrm{d}x + \mathcal{L}`)
	if styles != `bb(R) + bf(x) + dx + cal(L)` {
		t.Fatalf("unexpected style fallback: %s", styles)
	}

	relations := transformMathText(`\forall x \in A, \exists y \notin B, A \subseteq C, A \cup B, A \cap B`)
	if relations != `∀ x ∈ A, ∃ y ∉ B, A ⊆ C, A ∪ B, A ∩ B` {
		t.Fatalf("unexpected relation fallback: %s", relations)
	}

	ellipsis := transformMathText(`a_1, \ldots, a_n ; \cdots ; \vdots ; \ddots`)
	if ellipsis != `a<sub>1</sub>, …, a<sub>n</sub> ; ⋯ ; ⋮ ; ⋱` {
		t.Fatalf("unexpected ellipsis fallback: %s", ellipsis)
	}

	implications := transformMathText(`A \iff B, A \implies B, A \Rightarrow B, B \Leftarrow A`)
	if implications != `A ⇔ B, A ⇒ B, A ⇒ B, B ⇐ A` {
		t.Fatalf("unexpected implication fallback: %s", implications)
	}

	angles := transformMathText(`\langle x, y \rangle`)
	if angles != `⟨ x, y ⟩` {
		t.Fatalf("unexpected angle fallback: %s", angles)
	}

	setsAndLogic := transformMathText(`\Re z + \Im z + \emptyset + \varnothing + A \subset B + B \supset C + A \subsetneq C + P \land Q + P \lor Q + \neg P`)
	if setsAndLogic != `Re z + Im z + ∅ + ∅ + A ⊂ B + B ⊃ C + A ⊊ C + P ∧ Q + P ∨ Q + ¬ P` {
		t.Fatalf("unexpected set/logic fallback: %s", setsAndLogic)
	}

	reasons := transformMathText(`\because a=b, \therefore b=a`)
	if reasons != `∵ a=b, ∴ b=a` {
		t.Fatalf("unexpected reason fallback: %s", reasons)
	}

	magnitude := transformMathText(`|x| + ||y||`)
	if magnitude != `abs(x) + norm(y)` {
		t.Fatalf("unexpected magnitude fallback: %s", magnitude)
	}

	spacingAndRoots := transformMathText(`\sqrt[3]{x} + \dfrac{a}{b} + \tfrac{c}{d} + \quad y \qquad z`)
	if spacingAndRoots != `root(3, x) + <span style="white-space:nowrap">(a)/(b)</span> + <span style="white-space:nowrap">(c)/(d)</span> + y z` {
		t.Fatalf("unexpected root/fraction/spacing fallback: %s", spacingAndRoots)
	}

	floors := transformMathText(`\lfloor x \rfloor + \lceil y \rceil + a \mid b + A \parallel B`)
	if floors != `⌊ x ⌋ + ⌈ y ⌉ + a ∣ b + A ∥ B` {
		t.Fatalf("unexpected delimiter symbol fallback: %s", floors)
	}

	bigDelims := transformMathText(`\Bigl( x+y \Bigr) + \bigl[ z \bigr]`)
	if bigDelims != `( x+y ) + [ z ]` {
		t.Fatalf("unexpected big delimiter fallback: %s", bigDelims)
	}

	displayStyle := transformMathText(`\displaystyle \sum_{i=1}^{n} x_i`)
	if displayStyle != `∑<sub>i=1</sub><sup>n</sup> x<sub>i</sub>` {
		t.Fatalf("unexpected displaystyle fallback: %s", displayStyle)
	}

	modsAndArrows := transformMathText(`{n \choose k} + a \equiv b \pmod{m} + x \bmod y + \overrightarrow{AB} + \overleftarrow{CD}`)
	if modsAndArrows != `choose(n, k) + a ≡ b (mod m) + x mod y + overrightarrow(AB) + overleftarrow(CD)` {
		t.Fatalf("unexpected choose/mod/arrow fallback: %s", modsAndArrows)
	}

	bounds := transformMathText(`\limsup_{n\to\infty} a_n + \liminf_{n\to\infty} b_n + \sup_A f + \inf_B g`)
	if bounds != `limsup<sub>n→∞</sub> a<sub>n</sub> + liminf<sub>n→∞</sub> b<sub>n</sub> + sup<sub>A</sub> f + inf<sub>B</sub> g` {
		t.Fatalf("unexpected limsup/inf fallback: %s", bounds)
	}

	operatorsAndStats := transformMathText(`\gcd(a,b) + \det A + \dim V + \ker T + \Pr(X) + \mathbb{E}[X]`)
	if operatorsAndStats != `gcd(a,b) + det A + dim V + ker T + Pr(X) + bb(E)[X]` {
		t.Fatalf("unexpected operator/stat fallback: %s", operatorsAndStats)
	}

	moreArrows := transformMathText(`\overleftrightarrow{AB} + \underrightarrow{CD} + \underleftarrow{EF}`)
	if moreArrows != `overleftrightarrow(AB) + underrightarrow(CD) + underleftarrow(EF)` {
		t.Fatalf("unexpected extended arrow fallback: %s", moreArrows)
	}

	probabilityAndIntegrals := transformMathText(`\operatorname{Var}(X) + \operatorname{Cov}(X,Y) + \mathbb{P}(A) + \iint_D f + \iiint_V g + \oint_C h`)
	if probabilityAndIntegrals != `Var(X) + Cov(X,Y) + bb(P)(A) + ∬<sub>D</sub> f + ∭<sub>V</sub> g + ∮<sub>C</sub> h` {
		t.Fatalf("unexpected probability/integral fallback: %s", probabilityAndIntegrals)
	}

	negationsAndParens := transformMathText(`a \not= b + A \not\subseteq B + \overparen{AB} + \underparen{CD}`)
	if negationsAndParens != `a ≠ b + A ⊈ B + overparen(AB) + underparen(CD)` {
		t.Fatalf("unexpected negation/paren fallback: %s", negationsAndParens)
	}

	bigOperators := transformMathText(`\coprod_{i=1}^{n} A_i + \bigcup_{j=1}^{m} B_j + \bigcap_{k=1}^{p} C_k`)
	if bigOperators != `∐<sub>i=1</sub><sup>n</sup> A<sub>i</sub> + ⋃<sub>j=1</sub><sup>m</sup> B<sub>j</sub> + ⋂<sub>k=1</sub><sup>p</sup> C<sub>k</sub>` {
		t.Fatalf("unexpected big operator fallback: %s", bigOperators)
	}

	extensibleArrows := transformMathText(`\xrightarrow{n\to\infty} a_n + \xleftarrow{f} b`)
	if extensibleArrows != `xrightarrow(n→∞) a<sub>n</sub> + xleftarrow(f) b` {
		t.Fatalf("unexpected extensible arrow fallback: %s", extensibleArrows)
	}

	operatorStar := transformMathText(`\operatorname*{arg\,max}_{x} f(x)`)
	if operatorStar != `arg max<sub>x</sub> f(x)` {
		t.Fatalf("unexpected operator star fallback: %s", operatorStar)
	}

	moreBigOperators := transformMathText(`\bigoplus_{i=1}^{n} V_i + \bigotimes_{j=1}^{m} W_j + \bigodot_{k=1}^{p} U_k`)
	if moreBigOperators != `⨁<sub>i=1</sub><sup>n</sup> V<sub>i</sub> + ⨂<sub>j=1</sub><sup>m</sup> W<sub>j</sub> + ⨀<sub>k=1</sub><sup>p</sup> U<sub>k</sub>` {
		t.Fatalf("unexpected additional big operator fallback: %s", moreBigOperators)
	}

	xleftright := transformMathText(`\xleftrightarrow{A\iff B} x`)
	if xleftright != `xleftrightarrow(A⇔ B) x` {
		t.Fatalf("unexpected xleftrightarrow fallback: %s", xleftright)
	}

	moreNegations := transformMathText(`a \not\leq b + c \not\geq d + x \nsim y + p \ncong q`)
	if moreNegations != `a ≰ b + c ≱ d + x ≁ y + p ≇ q` {
		t.Fatalf("unexpected negated relation fallback: %s", moreNegations)
	}

	xmapstoCase := transformMathText(`\xmapsto{f} x + \overset{def}{\to} y + \underset{n\to\infty}{\to} z`)
	if xmapstoCase != `xmapsto(f) x + overset(def, →) y + underset(n→∞, →) z` {
		t.Fatalf("unexpected xmapsto/arrow label fallback: %s", xmapstoCase)
	}

	moreRelations := transformMathText(`A \subsetneqq B + C \supseteq D + E \supsetneq F + x \not\equiv y + p \not\sim q + m \not\cong n`)
	if moreRelations != `A ⫋ B + C ⊇ D + E ⊋ F + x ≢ y + p ≁ q + m ≇ n` {
		t.Fatalf("unexpected extended relation fallback: %s", moreRelations)
	}

	extraSymbols := transformMathText(`A \setminus B + x \mapsto y + y \gets z + a \propto b + l \perp m + P \ni x + u \oplus v + r \otimes s + p \ominus q + \angle ABC + \triangle DEF + X \Longrightarrow Y + U \Longleftrightarrow V`)
	if extraSymbols != `A ∖ B + x ↦ y + y ← z + a ∝ b + l ⊥ m + P ∋ x + u ⊕ v + r ⊗ s + p ⊖ q + ∠ ABC + △ DEF + X ⇒ Y + U ⇔ V` {
		t.Fatalf("unexpected extra symbol fallback: %s", extraSymbols)
	}

	moreStandaloneSymbols := transformMathText(`a \sim b + c \cong d + e \simeq f + g \asymp h + x \leftrightarrow y + p \uparrow q + r \downarrow s + \aleph + \hbar + \ell + \wp + \bullet + \circ + \star + \dagger + \ddagger + \clubsuit + \diamondsuit + \heartsuit + \spadesuit`)
	if moreStandaloneSymbols != `a ∼ b + c ≅ d + e ≃ f + g ≍ h + x ↔ y + p ↑ q + r ↓ s + ℵ + ℏ + ℓ + ℘ + • + ∘ + ⋆ + † + ‡ + ♣ + ♢ + ♡ + ♠` {
		t.Fatalf("unexpected standalone symbol fallback: %s", moreStandaloneSymbols)
	}

	logicAndRelations := transformMathText(`A \sqsubseteq B + C \sqsupseteq D + E \models F + G \vdash H + I \dashv J + \top + \bot + X \nsubseteq Y + M \nsupseteq N + \flat + \natural + \sharp + \bigvee_{i=1}^{n} P_i + \bigwedge_{j=1}^{m} Q_j + \bigsqcup_{k=1}^{p} R_k`)
	if logicAndRelations != `A ⊑ B + C ⊒ D + E ⊨ F + G ⊢ H + I ⊣ J + ⊤ + ⊥ + X ⊈ Y + M ⊉ N + ♭ + ♮ + ♯ + ⋁<sub>i=1</sub><sup>n</sup> P<sub>i</sub> + ⋀<sub>j=1</sub><sup>m</sup> Q<sub>j</sub> + ⨆<sub>k=1</sub><sup>p</sup> R<sub>k</sub>` {
		t.Fatalf("unexpected logic/relation fallback: %s", logicAndRelations)
	}

	moreRelationsAndArrows := transformMathText(`A \prec B + C \succ D + E \preceq F + G \succeq H + S \owns x + f \hookrightarrow X + Y \hookleftarrow g + a \updownarrow b + c \nearrow d + e \searrow f + g \swarrow h + i \nwarrow j`)
	if moreRelationsAndArrows != `A ≺ B + C ≻ D + E ≼ F + G ≽ H + S ∋ x + f ↪ X + Y ↩ g + a ↕ b + c ↗ d + e ↘ f + g ↙ h + i ↖ j` {
		t.Fatalf("unexpected relation/arrow fallback: %s", moreRelationsAndArrows)
	}

	triangleAndArrowVariants := transformMathText(`A \triangleleft B + C \triangleright D + E \trianglelefteq F + G \trianglerighteq H + u \twoheadrightarrow v + x \twoheadleftarrow y + p \rightsquigarrow q + r \leadsto s`)
	if triangleAndArrowVariants != `A ◃ B + C ▹ D + E ⊴ F + G ⊵ H + u ↠ v + x ↞ y + p ⇝ q + r ⇝ s` {
		t.Fatalf("unexpected triangle/arrow variant fallback: %s", triangleAndArrowVariants)
	}

	moreOrderAndParallelRelations := transformMathText(`a \ll b + c \gg d + e \lesssim f + g \gtrsim h + i \lessgtr j + k \gtrless l + m \shortmid n + p \nmid q + A \parallel B + C \nparallel D`)
	if moreOrderAndParallelRelations != `a ≪ b + c ≫ d + e ≲ f + g ≳ h + i ≶ j + k ≷ l + m ∣ n + p ∤ q + A ∥ B + C ∦ D` {
		t.Fatalf("unexpected order/parallel fallback: %s", moreOrderAndParallelRelations)
	}

	approxAndHarpoons := transformMathText(`a \lessapprox b + c \gtrapprox d + u \leftharpoonup v + w \rightharpoonup x + y \leftharpoondown z + p \rightharpoondown q`)
	if approxAndHarpoons != `a ⪅ b + c ⪆ d + u ↼ v + w ⇀ x + y ↽ z + p ⇁ q` {
		t.Fatalf("unexpected approx/harpoon fallback: %s", approxAndHarpoons)
	}

	moreApproxAndArrowVariants := transformMathText(`a \circeq b + c \eqcirc d + e \triangleq f + g \backsim h + i \backsimeq j + u \leftrightharpoons v + x \rightleftharpoons y + p \curvearrowleft q + r \curvearrowright s`)
	if moreApproxAndArrowVariants != `a ≗ b + c ≖ d + e ≜ f + g ∽ h + i ⋍ j + u ⇋ v + x ⇌ y + p ↶ q + r ↷ s` {
		t.Fatalf("unexpected approx/arrow variant fallback: %s", moreApproxAndArrowVariants)
	}

	dottedTriangleAndMultiMap := transformMathText(`a \doteq b + c \fallingdotseq d + e \risingdotseq f + g \vartriangleleft h + i \vartriangleright j + u \upuparrows v + x \downdownarrows y + p \multimap q + r \pitchfork s`)
	if dottedTriangleAndMultiMap != `a ≐ b + c ≒ d + e ≓ f + g ⊲ h + i ⊳ j + u ⇈ v + x ⇊ y + p ⊸ q + r ⋔ s` {
		t.Fatalf("unexpected dotted/triangle/multimap fallback: %s", dottedTriangleAndMultiMap)
	}

	moreEquivTrianglesAndSmiles := transformMathText(`a \eqsim b + c \bumpeq d + e \Bumpeq f + g \blacktriangleleft h + i \blacktriangleright j + u \circlearrowleft v + x \circlearrowright y + p \smallsmile q + r \smallfrown s`)
	if moreEquivTrianglesAndSmiles != `a ≂ b + c ≏ d + e ≎ f + g ◂ h + i ▸ j + u ↺ v + x ↻ y + p ⌣ q + r ⌢ s` {
		t.Fatalf("unexpected eq/triangle/smile fallback: %s", moreEquivTrianglesAndSmiles)
	}

	joinAndCurlyRelations := transformMathText(`A \Join B + C \bowtie D + E \curlywedge F + G \curlyvee H + I \doublebarwedge J + K \veebar L`)
	if joinAndCurlyRelations != `A ⋈ B + C ⋈ D + E ⋏ F + G ⋎ H + I ⌆ J + K ⊻ L` {
		t.Fatalf("unexpected join/curly fallback: %s", joinAndCurlyRelations)
	}

	moreLatticeRelations := transformMathText(`A \Cap B + C \Cup D + E \barwedge F + G \intercal H + I \divideontimes J`)
	if moreLatticeRelations != `A ⋒ B + C ⋓ D + E ⊼ F + G ⊺ H + I ⋇ J` {
		t.Fatalf("unexpected lattice fallback: %s", moreLatticeRelations)
	}

	squareAndCircledOperators := transformMathText(`A \sqcap B + C \sqcup D + E \ltimes F + G \rtimes H + I \leftthreetimes J + K \rightthreetimes L + M \circledast N + O \circledcirc P + Q \circleddash R + S \dotplus T`)
	if squareAndCircledOperators != `A ⊓ B + C ⊔ D + E ⋉ F + G ⋊ H + I ⋋ J + K ⋌ L + M ⊛ N + O ⊚ P + Q ⊝ R + S ∔ T` {
		t.Fatalf("unexpected square/circled fallback: %s", squareAndCircledOperators)
	}

	moreContainmentAndOrderRelations := transformMathText(`A \sqsubset B + C \sqsupset D + E \Subset F + G \Supset H + i \leqslant j + k \geqslant l + m \eqslantless n + p \eqslantgtr q`)
	if moreContainmentAndOrderRelations != `A ⊏ B + C ⊐ D + E ⋐ F + G ⋑ H + i ⩽ j + k ⩾ l + m ⪕ n + p ⪖ q` {
		t.Fatalf("unexpected containment/order fallback: %s", moreContainmentAndOrderRelations)
	}

	moreNegatedAndVariantRelations := transformMathText(`a \nleqslant b + c \ngeqslant d + e \precsim f + g \succsim h + i \precapprox j + k \succapprox l`)
	if moreNegatedAndVariantRelations != `a ⩽̸ b + c ⩾̸ d + e ≾ f + g ≿ h + i ⪷ j + k ⪸ l` {
		t.Fatalf("unexpected negated/variant fallback: %s", moreNegatedAndVariantRelations)
	}

	finalCommonRelationSweep := transformMathText(`A \nleq B + C \ngeq D + E \supsetneqq F + G \curlyeqprec H + I \curlyeqsucc J + K \nprec L + M \nsucc N + P \npreceq Q + R \nsucceq S + T \nsubset U + V \nsupset W`)
	if finalCommonRelationSweep != `A ≰ B + C ≱ D + E ⫌ F + G ⋞ H + I ⋟ J + K ⊀ L + M ⊁ N + P ⪯̸ Q + R ⪰̸ S + T ⊄ U + V ⊅ W` {
		t.Fatalf("unexpected final relation sweep fallback: %s", finalCommonRelationSweep)
	}

	braces := transformMathText(`\overbrace{a+b}^{sum} + \underbrace{x+y}_{pair}`)
	if braces != `overbrace(a+b, sum) + underbrace(x+y, pair)` {
		t.Fatalf("unexpected brace fallback: %s", braces)
	}

	textStyles := transformMathText(`\textbf{A} + \textit{B} + \emph{C}`)
	if textStyles != `bold(A) + italic(B) + emph(C)` {
		t.Fatalf("unexpected text style fallback: %s", textStyles)
	}

	delims := transformMathText(`\left[ x+y \right] + \left\{ a,b \right\} + \left|z\right|`)
	if delims != `[ x+y ] + a,b + abs(z)` {
		t.Fatalf("unexpected delimiter fallback: %s", delims)
	}
}

func TestMarkdownToHTML_ImageAndTableRendering(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(imagePath, []byte("not-a-real-image"), 0644); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	md := strings.Join([]string{
		"![系统架构图](" + filepath.ToSlash(imagePath) + ")",
		"",
		"| 模块 | 状态 | 备注 |",
		"| --- | --- | --- |",
		"| 用户模块 | 完成 | 已联调 |",
	}, "\n")
	html := markdownToHTML(md)
	if !strings.Contains(html, "<img src=") {
		t.Error("local image should render as img tag")
	}
	if strings.Contains(html, "![系统架构图]") {
		t.Error("raw markdown image syntax should not remain")
	}
	if strings.Contains(html, "| 模块 | 状态 | 备注 |") {
		t.Error("raw markdown table syntax should not remain")
	}
	if !strings.Contains(html, "<table>") {
		t.Error("table should render as html table")
	}
	if !strings.Contains(html, "<th>模块</th>") || !strings.Contains(html, "<th>状态</th>") || !strings.Contains(html, "<th>备注</th>") {
		t.Error("table headers should render as th cells")
	}
	if !strings.Contains(html, "<td>用户模块</td>") || !strings.Contains(html, "<td>完成</td>") || !strings.Contains(html, "<td>已联调</td>") {
		t.Error("table row should render as td cells")
	}
}

func TestMarkdownToHTML_RemoteImageFallback(t *testing.T) {
	md := "![系统架构图](https://example.com/diagram.png)"
	html := markdownToHTML(md)
	if strings.Contains(html, "![系统架构图](https://example.com/diagram.png)") {
		t.Error("raw remote image markdown should not remain")
	}
	if !strings.Contains(html, "暂不支持远程图片") {
		t.Error("remote image should use readable fallback")
	}
}

func TestMarkdownToHTML_MissingImageFallback(t *testing.T) {
	md := "![系统架构图](missing/diagram.png)"
	html := markdownToHTML(md)
	if strings.Contains(html, "![系统架构图](missing/diagram.png)") {
		t.Error("raw missing image markdown should not remain")
	}
	if !strings.Contains(html, "图片未找到") {
		t.Error("missing image should use readable fallback")
	}
}

func TestMarkdownToHTML_PipeTextNotTable(t *testing.T) {
	md := "普通文本里有 | 符号，但不是表格"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<p>普通文本里有 | 符号，但不是表格</p>") {
		t.Error("plain pipe text should remain a paragraph")
	}
}

func TestMarkdownToHTML_Bold(t *testing.T) {
	md := "这是**粗体**文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<b>粗体</b>") {
		t.Error("should convert **text** to <b>text</b>")
	}
}

func TestMarkdownToHTML_Italic(t *testing.T) {
	md := "这是*斜体*文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<i>斜体</i>") {
		t.Error("should convert *text* to <i>text</i>")
	}
}

func TestMarkdownToHTML_HorizontalRule(t *testing.T) {
	md := "上面\n---\n下面"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<hr/>") {
		t.Error("should convert --- to <hr/>")
	}
}

func TestMarkdownToHTML_Paragraph(t *testing.T) {
	md := "普通段落文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<p>普通段落文本</p>") {
		t.Error("should wrap plain text in <p>")
	}
}

func TestMarkdownToHTML_HTMLEscape(t *testing.T) {
	md := "包含 <script> 标签"
	html := markdownToHTML(md)
	if strings.Contains(html, "<script>") {
		t.Error("should escape HTML tags")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("should contain escaped HTML")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"a & b", "a &amp; b"},
		{"<div>", "&lt;div&gt;"},
	}
	for _, tt := range tests {
		got := escapeHTML(tt.input)
		if got != tt.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestInlineMD(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"**bold**", "<b>bold</b>"},
		{"*italic*", "<i>italic</i>"},
		{"normal", "normal"},
		{"**a** and *b*", "<b>a</b> and <i>b</i>"},
	}
	for _, tt := range tests {
		got := inlineMD(tt.input)
		if got != tt.want {
			t.Errorf("inlineMD(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"my-project", "my-project"},
		{"/path/to/project", "project"},
		{"a b c", "a_b_c"},
		{"file<>name", "file_name"},
	}
	for _, tt := range tests {
		got := sanitizeFileName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeFileName_Long(t *testing.T) {
	long := strings.Repeat("a", 50)
	got := sanitizeFileName(long)
	if len(got) > 30 {
		t.Errorf("should truncate to 30 chars, got %d", len(got))
	}
}

func TestFileNameForSpecDefaultPrefixUsesStableASCII(t *testing.T) {
	got := fileNameForSpec(Spec{
		ProjectName: "项目A",
		Title:       "文档",
		Timestamp:   time.Date(2026, 5, 11, 10, 30, 0, 0, time.UTC),
	})
	if !strings.HasPrefix(got, "document_") {
		t.Fatalf("default filename prefix = %q, want stable ASCII document prefix", got)
	}
	if strings.HasPrefix(got, "文档_") {
		t.Fatalf("default filename prefix should not be localized display text: %q", got)
	}
	if strings.Contains(got, "项目") || strings.Contains(got, "文档") {
		t.Fatalf("filename should not contain localized project/title text: %q", got)
	}
}

func TestASCIIFileNameSegment(t *testing.T) {
	tests := []struct {
		input    string
		fallback string
		want     string
	}{
		{input: "My Project", fallback: "document", want: "my_project"},
		{input: "需求文档", fallback: "document", want: "document"},
		{input: "需求文档", fallback: "文档", want: "document"},
		{input: "/tmp/Feature 01.md", fallback: "document", want: "feature_01_md"},
		{input: "report<>name", fallback: "document", want: "reportname"},
	}
	for _, tt := range tests {
		if got := asciiFileNameSegment(tt.input, tt.fallback); got != tt.want {
			t.Fatalf("asciiFileNameSegment(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
		}
	}
}

func TestNormalizePaperSize(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "a4", false},
		{"A4", "a4", false},
		{"b5", "b5", false},
		{"B5", "b5", false},
		{"a5", "", true},
	}
	for _, tt := range tests {
		got, err := normalizePaperSize(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizePaperSize(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizePaperSize(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizePaperSize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolvePDFPageLayout(t *testing.T) {
	a4, err := resolvePDFPageLayout("")
	if err != nil {
		t.Fatalf("default layout error: %v", err)
	}
	b5, err := resolvePDFPageLayout("b5")
	if err != nil {
		t.Fatalf("b5 layout error: %v", err)
	}
	if a4.pageW <= b5.pageW {
		t.Fatalf("expected A4 width > B5 width, got %.2f <= %.2f", a4.pageW, b5.pageW)
	}
	if a4.pageH <= b5.pageH {
		t.Fatalf("expected A4 height > B5 height, got %.2f <= %.2f", a4.pageH, b5.pageH)
	}
	if a4.contentW <= b5.contentW {
		t.Fatalf("expected A4 content width > B5 content width, got %.2f <= %.2f", a4.contentW, b5.contentW)
	}
}

func TestSplitOversizedMarkdownBlock(t *testing.T) {
	block := strings.Repeat("这是一段很长的文本，用来测试超大块拆分。", 80)
	parts := splitOversizedMarkdownBlock(block)
	if len(parts) < 2 {
		t.Fatalf("expected oversized block to split, got %d parts", len(parts))
	}
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			t.Fatalf("part %d should not be empty", i)
		}
	}
}

func TestSplitMarkdownBlockForFilling(t *testing.T) {
	block := "这是第一句。这是第二句。这是第三句。"
	parts := splitMarkdownBlockForFilling(block)
	if len(parts) < 2 {
		t.Fatalf("expected block to split into smaller parts, got %d", len(parts))
	}
}

func TestSplitMarkdownBlockForFilling_TableKeepsWhole(t *testing.T) {
	block := strings.Join([]string{
		"| 模块 | 状态 |",
		"| --- | --- |",
		"| 用户模块 | 完成 |",
		"| 支付模块 | 进行中 |",
	}, "\n")
	parts := splitMarkdownBlockForFilling(block)
	if len(parts) != 1 {
		t.Fatalf("expected table block to remain whole, got %d parts", len(parts))
	}
	if parts[0] != block {
		t.Fatalf("expected table block to stay unchanged, got %q", parts[0])
	}
}

func TestSplitParagraphForFilling_CommaFallback(t *testing.T) {
	text := "需求分析，方案设计，接口联调，回归验证"
	parts := splitParagraphForFilling(text)
	if len(parts) < 2 {
		t.Fatalf("expected comma-based split, got %d", len(parts))
	}
}

func TestSplitParagraphForFilling_ListItemKeepsMarker(t *testing.T) {
	text := "1. 子任务一：准备输入内容。"
	parts := splitParagraphForFilling(text)
	if len(parts) != 1 {
		t.Fatalf("expected list item to remain whole, got %d parts", len(parts))
	}
	if parts[0] != text {
		t.Fatalf("expected list item to stay unchanged, got %q", parts[0])
	}
}
