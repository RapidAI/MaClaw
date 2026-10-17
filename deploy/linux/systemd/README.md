# Hub and Hub Center systemd units

These match the units on the production hosts. `start.sh` in each install directory restarts the unit when it is installed, and does not launch a second process.

Ubuntu 18.04 ships systemd 237, which cannot parse `StandardOutput=append:`. Hub appends `data/logs/maclaw-hub.out.log` from `run-systemd.sh`. Hub Center logs go to the journal.

## Hub

From the repo root, on the host:

```bash
cp hub/run-systemd.sh /data/soft/hub/run-systemd.sh
sed -i 's/\r$//' /data/soft/hub/run-systemd.sh
chmod 755 /data/soft/hub/run-systemd.sh
cp deploy/linux/systemd/maclaw-hub.service.example /etc/systemd/system/maclaw-hub.service
systemctl daemon-reload
systemctl enable --now maclaw-hub.service
systemctl status maclaw-hub.service --no-pager
```

`desktop-api.env` in `/data/soft/hub`, when present, is sourced by `run-systemd.sh`. The unit's `ExecStart` must stay `/data/soft/hub/run-systemd.sh`.

Later deploys can restart with either of:

```bash
systemctl restart maclaw-hub
cd /data/soft/hub && ./start.sh
```

## Hub Center

```bash
cp deploy/linux/systemd/maclaw-hubcenter.service.example /etc/systemd/system/maclaw-hubcenter.service
systemctl daemon-reload
systemctl enable --now maclaw-hubcenter.service
systemctl status maclaw-hubcenter.service --no-pager
```

Restart with `systemctl restart maclaw-hubcenter` or `/data/soft/hubcenter/start.sh`.
