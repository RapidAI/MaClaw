#!/bin/sh
# Started by XFCE autostart: wallpaper, compositor, dock (in that order).
feh --no-fehbg --bg-fill /usr/share/backgrounds/maclaw/gray-gradient.png 2>/dev/null \
  || xsetroot -solid '#d6d9de'
pgrep -u "$(id -u)" -x picom >/dev/null || picom --config /etc/xdg/picom.conf --daemon
sleep 1
pgrep -u "$(id -u)" -x plank >/dev/null || exec plank
