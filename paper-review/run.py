#!/usr/bin/env python3
"""
Paper Review 鈥?璁烘枃娣卞害瑙ｈ涓诲叆鍙?绔埌绔祦绋嬶細涓嬭浇PDF 鈫?鎻愬彇鏂囨湰 鈫?LLM瑙ｈ 鈫?鎻愬彇鍥剧墖 鈫?鐢熸垚PPT 鈫?鐢熸垚闊抽
"""

import argparse, os, sys, glob, subprocess, json, time

STEPS_DIR = os.path.dirname(os.path.abspath(__file__))

class StepRunner:
    def __init__(self, base_dir, output_dir):
        self.base_dir = os.path.abspath(base_dir)
        self.output_dir = os.path.abspath(output_dir)
        self.results = {}
        os.makedirs(self.output_dir, exist_ok=True)

    def run_python(self, script, args_list, step_name, timeout=300):
        """Run a Python script with args."""
        script_path = os.path.join(STEPS_DIR, script)
        cmd = [sys.executable, script_path] + args_list

        print(f"\n{'='*60}")
        print(f"[Step] {step_name}")
        print(f"{'='*60}")
        print(f"Running: {' '.join(cmd)}")

        try:
            result = subprocess.run(
                cmd, capture_output=True, text=True, timeout=timeout
            )

            # Print stdout (parsed for key outputs)
            for line in result.stdout.split("\n"):
                line = line.strip()
                if line and not line.startswith("WARNING") and not line.startswith("INFO"):
                    # Print key output lines
                    if any(kw in line for kw in ["SAVED", "PATH:", "DONE", "ERROR", "Total", "Downloading", "Analyzing", "Generating", "Extracting"]):
                        print(f"  {line}")

            if result.stderr:
                for line in result.stderr.strip().split("\n"):
                    if line.strip():
                        print(f"  [stderr] {line.strip()}")

            self.results[step_name] = {
                "success": result.returncode == 0,
                "stdout": result.stdout,
                "stderr": result.stderr
            }

            return result.returncode == 0, result.stdout

        except subprocess.TimeoutExpired:
            print(f"  [TIMEOUT] Step '{step_name}' exceeded {timeout}s")
            self.results[step_name] = {"success": False, "error": f"Timeout ({timeout}s)"}
            return False, ""
        except Exception as e:
            print(f"  [ERROR] Step '{step_name}' failed: {e}")
            self.results[step_name] = {"success": False, "error": str(e)}
            return False, ""

    def extract_output_paths(self, stdout: str) -> dict:
        """Extract output file paths from step stdout."""
        paths = {}
        for line in stdout.split("\n"):
            for prefix in ["PDF_PATH:", "TEXT_PATH:", "REVIEW_JSON:", "REVIEW_MD:", "PPT_PATH:", "AUDIO_PATH:", "IMAGES_DIR:"]:
                if prefix in line:
                    path = line.split(prefix, 1)[1].strip()
                    paths[prefix.replace(":", "").lower()] = path
        return paths

    def find_output_files(self, pattern: str) -> list:
        """Find files matching glob pattern in output dir."""
        return sorted(glob.glob(os.path.join(self.output_dir, pattern)))

    def run_all(self, paper_url: str):
        """Run all steps sequentially."""
        results_summary = []

        # Step 1: Download PDF
        success, stdout = self.run_python(
            "download_paper.py", ["--url", paper_url, "--output", self.output_dir],
            "涓嬭浇璁烘枃PDF", timeout=120
        )
        paths = self.extract_output_paths(stdout)
        pdf_path = paths.get("pdf_path", "")
        if not pdf_path:
            pdfs = self.find_output_files("*.pdf")
            if pdfs:
                pdf_path = pdfs[0]
        results_summary.append(("Step 1: 涓嬭浇璁烘枃", "鉁? if success else "鉂?, pdf_path))

        if not success or not pdf_path:
            self.print_summary(results_summary)
            return False

        # Step 2: Extract text
        success, stdout = self.run_python(
            "extract_text.py", ["--pdf", pdf_path, "--output", self.output_dir],
            "PDF鏂囨湰鎻愬彇", timeout=300
        )
        paths = self.extract_output_paths(stdout)
        txt_path = paths.get("text_path", "")
        if not txt_path:
            txts = self.find_output_files("*_text.txt")
            if txts:
                txt_path = txts[0]
        results_summary.append(("Step 2: 鏂囨湰鎻愬彇", "鉁? if success else "鉂?, txt_path))

        if not success or not txt_path:
            self.print_summary(results_summary)
            return False

        # Step 3: LLM Analysis
        success, stdout = self.run_python(
            "analyze_paper.py", ["--text", txt_path, "--output", self.output_dir],
            "LLM娣卞害瑙ｈ", timeout=600
        )
        paths = self.extract_output_paths(stdout)
        json_path = paths.get("review_json", "")
        md_path = paths.get("review_md", "")
        if not json_path:
            jsons = self.find_output_files("*_review.json")
            if jsons:
                json_path = jsons[0]
        if not md_path:
            mds = self.find_output_files("*_review.md")
            if mds:
                md_path = mds[0]
        results_summary.append(("Step 3: LLM娣卞害瑙ｈ", "鉁? if success else "鉂?, md_path))

        if not success or not json_path:
            self.print_summary(results_summary)
            return False

        # Step 4: Extract images
        success, stdout = self.run_python(
            "extract_images.py", ["--pdf", pdf_path, "--output", os.path.join(self.output_dir, "images")],
            "鍥剧墖绱犳潗鎻愬彇", timeout=300
        )
        paths = self.extract_output_paths(stdout)
        images_dir = paths.get("images_dir", os.path.join(self.output_dir, "images"))
        img_count = len(glob.glob(os.path.join(images_dir, "*.png"))) if os.path.isdir(images_dir) else 0
        results_summary.append(("Step 4: 鍥剧墖鎻愬彇", f"鉁?({img_count}寮?" if os.path.isdir(images_dir) else "鈿狅笍", images_dir))

        # Step 5: Generate PPT
        success, stdout = self.run_python(
            "generate_ppt.py", ["--review", json_path, "--images", images_dir, "--output", self.output_dir],
            "鐢熸垚缁勪細PPT", timeout=300
        )
        paths = self.extract_output_paths(stdout)
        ppt_path = paths.get("ppt_path", "")
        if not ppt_path:
            ppts = self.find_output_files("*_presentation.pptx")
            if ppts:
                ppt_path = ppts[0]
        results_summary.append(("Step 5: 鐢熸垚PPT", "鉁? if success and ppt_path else "鉂?, ppt_path))

        # Step 6: Generate Audio
        if md_path:
            success, stdout = self.run_python(
                "generate_audio.py", ["--text", md_path, "--output", self.output_dir],
                "鐢熸垚瑙ｈ闊抽", timeout=300
            )
        paths = self.extract_output_paths(stdout)
        audio_path = paths.get("audio_path", "")
        if not audio_path:
            audios = self.find_output_files("*_audio.mp3")
            if audios:
                audio_path = audios[0]
        results_summary.append(("Step 6: 鐢熸垚闊抽", "鉁? if audio_path else "鈿狅笍", audio_path))

        self.print_summary(results_summary)
        return True

    def print_summary(self, results):
        """Print final summary."""
        print(f"\n{'='*60}")
        print(f"馃搵 璁烘枃瑙ｈ瀹屾垚 鈥?杈撳嚭鐩綍: {self.output_dir}")
        print(f"{'='*60}")
        for step, status, path in results:
            path_str = f" 鈫?{os.path.relpath(path, self.output_dir)}" if path else ""
            print(f"  {status} {step}{path_str}")
        print(f"{'='*60}")

        # Save summary JSON
        summary = {
            "status": "completed",
            "output_dir": self.output_dir,
            "steps": [
                {"step": s, "status": st, "path": p} for s, st, p in results
            ],
            "timestamp": time.strftime("%Y-%m-%d %H:%M:%S")
        }
        summary_path = os.path.join(self.output_dir, "_summary.json")
        with open(summary_path, "w", encoding="utf-8") as f:
            json.dump(summary, f, ensure_ascii=False, indent=2)


def main():
    parser = argparse.ArgumentParser(description="Paper Review 鈥?绔埌绔鏂囨繁搴﹁В璇?)
    parser.add_argument("--url", required=True, help="璁烘枃URL (arXiv/DOI/鐩存帴PDF閾炬帴)")
    parser.add_argument("--output", default=".", help="杈撳嚭鐩綍")
    parser.add_argument("--skip-audio", action="store_true", help="璺宠繃闊抽鐢熸垚")
    parser.add_argument("--skip-images", action="store_true", help="璺宠繃鍥剧墖鎻愬彇")
    args = parser.parse_args()

    runner = StepRunner(os.path.dirname(os.path.abspath(__file__)), args.output)
    success = runner.run_all(args.url)

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
