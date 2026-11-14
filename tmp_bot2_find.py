import os
import sys

import paramiko

pw = os.environ.get("HUB_PW")
if not pw:
    sys.exit("HUB_PW unset")

cli = paramiko.SSHClient()
cli.set_missing_host_key_policy(paramiko.AutoAddPolicy())
cli.connect("hub.mypapers.top", username="root", password=pw, timeout=30, banner_timeout=30)


def run(cmd, timeout=60):
    _, stdout, stderr = cli.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode("utf-8", "replace")
    err = stderr.read().decode("utf-8", "replace")
    code = stdout.channel.recv_exit_status()
    print(f"$ {cmd}\nexit={code}\n{out}")
    if err.strip():
        print("ERR", err[:2000])
    return code, out


run("date; ls -lt /data/soft/maclaw_srv/.maclaw/logs 2>/dev/null | head -20; ls -lt /root/.maclaw/logs 2>/dev/null | head -10")
run("journalctl -u maclawsrv.service --since '2026-11-14 18:00:00' --no-pager -o cat 2>/dev/null | grep -E '2096778682895540557|桌面环境|Bot 2|bot_2' | tail -40")
cli.close()
