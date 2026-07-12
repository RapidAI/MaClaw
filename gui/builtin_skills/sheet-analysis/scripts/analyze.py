"""Spreadsheet analysis: statistics, distribution, outliers, and visualization."""
import argparse
import os
import sys


def load_dataframe(input_path: str):
    """Load Excel or CSV into a pandas DataFrame."""
    try:
        import pandas as pd
    except ImportError:
        print("ERROR: pandas not installed. Run: pip install pandas openpyxl", file=sys.stderr)
        sys.exit(1)

    ext = os.path.splitext(input_path)[1].lower()
    if ext in ('.xlsx', '.xls'):
        try:
            return pd.read_excel(input_path)
        except Exception as e:
            print(f"ERROR: Failed to read Excel: {e}", file=sys.stderr)
            sys.exit(1)
    elif ext == '.csv':
        # Try common encodings — only catch encoding errors, not other failures
        last_err = None
        for enc in ('utf-8', 'gbk', 'gb2312', 'latin-1'):
            try:
                return pd.read_csv(input_path, encoding=enc)
            except UnicodeDecodeError:
                continue
            except Exception as e:
                last_err = e
                break  # Non-encoding error — don't try other encodings
        msg = f"Failed to read CSV: {last_err}" if last_err else "Failed to read CSV with any encoding"
        print(f"ERROR: {msg}", file=sys.stderr)
        sys.exit(1)
    else:
        print(f"ERROR: Unsupported format: {ext}. Use .xlsx, .xls, or .csv", file=sys.stderr)
        sys.exit(1)


def generate_report(df, output_dir: str, base_name: str) -> str:
    """Generate markdown analysis report."""
    import pandas as pd
    
    lines = [f"# 数据分析报告: {base_name}\n"]
    
    # Basic info
    lines.append("## 1. 数据概览\n")
    lines.append(f"- **行数**: {len(df)}")
    lines.append(f"- **列数**: {len(df.columns)}")
    lines.append(f"- **列名**: {', '.join(str(c) for c in df.columns)}")

    if len(df) == 0:
        lines.append("\n[WARN] 数据为空（0 行），无法生成统计分析。")
        report_text = "\n".join(lines)
        report_path = os.path.join(output_dir, f"{base_name}_analysis.md")
        with open(report_path, 'w', encoding='utf-8') as f:
            f.write(report_text)
        return report_path
    
    # Missing values
    missing = df.isnull().sum()
    if missing.any():
        lines.append(f"\n### 缺失值统计\n")
        lines.append("| 列名 | 缺失数 | 缺失率 |")
        lines.append("|------|--------|--------|")
        for col in df.columns:
            if missing[col] > 0:
                rate = f"{missing[col] / len(df) * 100:.1f}%"
                lines.append(f"| {col} | {missing[col]} | {rate} |")
    
    # Numeric columns statistics
    numeric_cols = df.select_dtypes(include=['number']).columns.tolist()
    if numeric_cols:
        lines.append("\n## 2. 数值列统计\n")
        lines.append("| 列名 | 均值 | 中位数 | 标准差 | 最小值 | 最大值 |")
        lines.append("|------|------|--------|--------|--------|--------|")
        for col in numeric_cols:
            s = df[col].dropna()
            if len(s) == 0:
                continue
            lines.append(f"| {col} | {s.mean():.2f} | {s.median():.2f} | {s.std():.2f} | {s.min():.2f} | {s.max():.2f} |")
        
        # Outlier detection (IQR method)
        lines.append("\n## 3. 异常值检测 (IQR 方法)\n")
        outlier_found = False
        for col in numeric_cols:
            s = df[col].dropna()
            if len(s) < 4:
                continue
            q1, q3 = s.quantile(0.25), s.quantile(0.75)
            iqr = q3 - q1
            if iqr == 0:
                continue
            outliers = s[(s < q1 - 1.5 * iqr) | (s > q3 + 1.5 * iqr)]
            if len(outliers) > 0:
                outlier_found = True
                lines.append(f"- **{col}**: {len(outliers)} 个异常值 "
                           f"(范围: [{q1 - 1.5*iqr:.2f}, {q3 + 1.5*iqr:.2f}] 之外)")
        if not outlier_found:
            lines.append("未检测到明显异常值。")
    
    # Categorical columns
    cat_cols = df.select_dtypes(include=['object', 'category']).columns.tolist()
    if cat_cols:
        lines.append("\n## 4. 分类列分布\n")
        for col in cat_cols[:5]:  # Limit to first 5
            vc = df[col].value_counts().head(10)
            lines.append(f"\n### {col} (前10)\n")
            lines.append("| 值 | 计数 | 占比 |")
            lines.append("|------|------|------|")
            for val, cnt in vc.items():
                rate = f"{cnt / len(df) * 100:.1f}%"
                lines.append(f"| {val} | {cnt} | {rate} |")
    
    # Correlation (top pairs)
    if len(numeric_cols) >= 2:
        lines.append("\n## 5. 相关性分析（前5强相关对）\n")
        corr = df[numeric_cols].corr()
        pairs = []
        for i in range(len(numeric_cols)):
            for j in range(i + 1, len(numeric_cols)):
                c = abs(corr.iloc[i, j])
                if c < 1.0:  # skip self
                    pairs.append((numeric_cols[i], numeric_cols[j], corr.iloc[i, j]))
        pairs.sort(key=lambda x: abs(x[2]), reverse=True)
        if pairs:
            lines.append("| 列 A | 列 B | 相关系数 |")
            lines.append("|------|------|----------|")
            for a, b, c in pairs[:5]:
                lines.append(f"| {a} | {b} | {c:.3f} |")
    
    report_text = "\n".join(lines)
    report_path = os.path.join(output_dir, f"{base_name}_analysis.md")
    with open(report_path, 'w', encoding='utf-8') as f:
        f.write(report_text)
    
    return report_path


def generate_charts(df, output_dir: str, base_name: str) -> list:
    """Generate basic visualization charts."""
    try:
        import matplotlib
        matplotlib.use('Agg')
        import matplotlib.pyplot as plt
    except ImportError:
        return []

    # Set Chinese font support
    plt.rcParams['font.sans-serif'] = ['SimHei', 'DejaVu Sans', 'Arial']
    plt.rcParams['axes.unicode_minus'] = False

    charts = []
    numeric_cols = df.select_dtypes(include=['number']).columns.tolist()

    # Distribution histogram for numeric columns (max 4)
    if numeric_cols:
        fig, axes = plt.subplots(1, min(4, len(numeric_cols)), 
                                  figsize=(4 * min(4, len(numeric_cols)), 4))
        if len(numeric_cols) == 1:
            axes = [axes]
        for i, col in enumerate(numeric_cols[:4]):
            axes[i].hist(df[col].dropna(), bins=20, edgecolor='black', alpha=0.7)
            axes[i].set_title(str(col))
            axes[i].set_xlabel('')
        plt.tight_layout()
        chart_path = os.path.join(output_dir, f"{base_name}_distribution.png")
        plt.savefig(chart_path, dpi=100, bbox_inches='tight')
        plt.close()
        charts.append(chart_path)

    return charts


def main():
    parser = argparse.ArgumentParser(description="Analyze spreadsheet data")
    parser.add_argument("--input", required=True, help="Input file (Excel/CSV)")
    parser.add_argument("--output", default=".", help="Output directory")
    args = parser.parse_args()

    input_path = args.input
    if not os.path.isfile(input_path):
        print(f"ERROR: File not found: {input_path}", file=sys.stderr)
        sys.exit(1)

    output_dir = args.output
    os.makedirs(output_dir, exist_ok=True)
    base_name = os.path.splitext(os.path.basename(input_path))[0]

    print(f"正在分析: {input_path}")
    df = load_dataframe(input_path)
    print(f"   数据维度: {df.shape[0]} 行 × {df.shape[1]} 列")

    report_path = generate_report(df, output_dir, base_name)
    print(f"   分析报告: {report_path}")

    charts = generate_charts(df, output_dir, base_name)
    if charts:
        for chart in charts:
            print(f"   可视化: {chart}")

    print(f"\n分析完成，结果已保存到: {os.path.abspath(output_dir)}")


if __name__ == "__main__":
    main()
