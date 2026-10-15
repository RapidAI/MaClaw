#!/bin/sh
# Started by XFCE autostart: wallpaper, compositor, dock (in that order).
xsetroot -solid '#d6d9de'
# Theme variant (wallpaper, GTK/xfwm4/icon theme, Plank theme, top bar).
/usr/bin/maclaw-theme-variant
pgrep -u "$(id -u)" -x picom >/dev/null || picom --config /etc/xdg/picom.conf --daemon
sleep 1
# Bot displays run their own Plank (per-bot dock); only a dock on this
# DISPLAY counts.
for p in $(pgrep -u "$(id -u)" -x plank); do
  tr '\0' '\n' </proc/$p/environ 2>/dev/null | grep -qx "DISPLAY=$DISPLAY" && exit 0
done
exec plank
