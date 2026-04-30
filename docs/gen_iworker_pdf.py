# -*- coding: utf-8 -*-
"""iWorker 宣传稿 MD → PDF 转换脚本"""
import subprocess, sys, os

md2pdf_script = r"C:\Users\ma139\.maclaw\data\skills\lovstudio_any2pdf\scripts\md2pdf.py"
input_md = r"D:\workprj\aicoder\docs\iworker-客户宣传稿_v1.md"
output_pdf = r"D:\workprj\aicoder\docs\iworker-客户宣传稿_v1.pdf"

cmd = [
    sys.executable, md2pdf_script,
    "--input", input_md,
    "--output", output_pdf,
    "--title", "iWorker 企业 AI Native 组织运行系统",
    "--subtitle", "客户宣传稿",
    "--author", "琢光智能",
    "--theme", "warm-academic",
    "--page-size", "A4",
    "--edition-line", "V1.0 | 2025",
    "--copyright", "© 2025 琢光智能 版权所有",
]

print(f"Running: {' '.join(cmd)}")
result = subprocess.run(cmd, capture_output=True, text=True, encoding='utf-8')
print(result.stdout)
if result.stderr:
    print(result.stderr)
print(f"Exit code: {result.returncode}")
if os.path.exists(output_pdf):
    size_kb = os.path.getsize(output_pdf) / 1024
    print(f"PDF generated successfully: {output_pdf} ({size_kb:.1f} KB)")
else:
    print("ERROR: PDF file not found!")
