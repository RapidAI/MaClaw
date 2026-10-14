#!/bin/sh
# maclaw-theme-variant <clean|dark|colorful|topbar>
# Applies a desktop look to the running XFCE session (live) and remembers it
# in ~/.config/maclaw-theme. Without an argument: the remembered variant,
# else /etc/maclaw/theme, else "clean".
v="$1"
[ -n "$v" ] || v=$(cat "$HOME/.config/maclaw-theme" 2>/dev/null)
[ -n "$v" ] || v=$(cat /etc/maclaw/theme 2>/dev/null)
[ -n "$v" ] || v=clean
W=/usr/share/backgrounds/maclaw
case "$v" in
  dark)     gtk=Arc-Dark; wm=Arc-Dark; icons=Papirus-Dark; wall=$W/maclaw-dark.jpg;     plank=MaClaw-Dark;  panel=0 ;;
  colorful) gtk=Arc;      wm=Arc;      icons=Papirus;      wall=$W/maclaw-colorful.jpg; plank=MaClaw-Light; panel=0 ;;
  topbar)   gtk=Arc;      wm=Arc;      icons=Papirus;      wall=$W/maclaw-colorful.jpg; plank=MaClaw-Light; panel=1 ;;
  *)        v=clean; gtk=Arc; wm=Arc;  icons=Papirus;      wall=$W/gray-gradient.png;   plank=Transparent;  panel=0 ;;
esac
mkdir -p "$HOME/.config"; echo "$v" > "$HOME/.config/maclaw-theme"
q() { xfconf-query -c "$1" -p "$2" -n -t string -s "$3" 2>/dev/null || xfconf-query -c "$1" -p "$2" -s "$3"; }
q xsettings /Net/ThemeName "$gtk"
q xsettings /Net/IconThemeName "$icons"
q xfwm4 /general/theme "$wm"
dconf write /net/launchpad/plank/docks/dock1/theme "'$plank'" 2>/dev/null
feh --no-fehbg --bg-fill "$wall" 2>/dev/null
if [ "$panel" = 1 ]; then
  pgrep -x xfce4-panel >/dev/null || (setsid xfce4-panel >/dev/null 2>&1 &)
else
  pgrep -x xfce4-panel >/dev/null && xfce4-panel --quit 2>/dev/null
fi
exit 0
