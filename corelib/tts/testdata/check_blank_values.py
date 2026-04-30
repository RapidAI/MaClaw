#!/usr/bin/env python3
"""Check m_p and logs_p values at blank vs non-blank positions."""
import os, numpy as np

outdir = os.path.dirname(os.path.abspath(__file__))
m_p = np.fromfile(os.path.join(outdir, "ref_05_m_p.bin"), dtype=np.float32).reshape(192, 9)
logs_p = np.fromfile(os.path.join(outdir, "ref_06_logs_p.bin"), dtype=np.float32).reshape(192, 9)

# phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]
# Blank positions: 0, 2, 4, 6, 8
# Non-blank: 1, 3, 5, 7

print("=== m_p per time step ===")
for t in range(9):
    col = m_p[:, t]
    is_blank = "BLANK" if t % 2 == 0 else "phone"
    print(f"  t={t} ({is_blank}): mean={col.mean():.2f}, std={col.std():.2f}, "
          f"min={col.min():.2f}, max={col.max():.2f}")

print("\n=== logs_p per time step ===")
for t in range(9):
    col = logs_p[:, t]
    is_blank = "BLANK" if t % 2 == 0 else "phone"
    print(f"  t={t} ({is_blank}): mean={col.mean():.2f}, std={col.std():.2f}, "
          f"min={col.min():.2f}, max={col.max():.2f}")

print("\n=== After expansion (durations=[4,4,3,3,3,3,3,4,4]) ===")
durations = [4, 4, 3, 3, 3, 3, 3, 4, 4]
print(f"Blank tokens expand to: {sum(durations[i] for i in range(0,9,2))} mel frames")
print(f"Phone tokens expand to: {sum(durations[i] for i in range(1,9,2))} mel frames")
