# Bake the per-user supervisor into the GUI image and keep profiles on /desktops.
# The browser is Chromium so a login made in noVNC stays in the profile the
# agent continues. maclaw-gui:1 must already exist. From the repo root:
#   docker build -t maclaw-gui:1 desktopd/image
FROM maclaw-gui:1
USER root
RUN apt-get update \
    && (apt-get install -y --no-install-recommends \
        xvfb fluxbox xdotool x11vnc novnc python3 python3-websockify \
        fonts-noto-cjk chromium \
        || apt-get install -y --no-install-recommends \
        xvfb fluxbox xdotool x11vnc novnc python3 python3-websockify \
        fonts-noto-cjk chromium-browser) \
    && rm -rf /var/lib/apt/lists/*
COPY desktop_supervisor.py /desktop_supervisor.py
VOLUME ["/desktops"]
