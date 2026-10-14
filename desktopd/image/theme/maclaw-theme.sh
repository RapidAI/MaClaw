#!/bin/sh
# Started by XFCE autostart: wallpaper, compositor, dock (in that order).
xsetroot -solid '#d6d9de'
# Theme variant (wallpaper, GTK/xfwm4/icon theme, Plank theme, top bar).
/usr/bin/maclaw-theme-variant
pgrep -u "$(id -u)" -x picom >/dev/null || picom --config /etc/xdg/picom.conf --daemon
sleep 1
pgrep -u "$(id -u)" -x plank >/dev/null || exec plank
