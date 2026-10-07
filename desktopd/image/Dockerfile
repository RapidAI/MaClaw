# Legacy overlay for maclaw-gui:1 only (fluxbox desktop). New deployments use
# Dockerfile.v2, which builds maclaw-gui:2 from Debian directly; never tag a
# build of this file as maclaw-gui:2.
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
# Browser entries join the supervised Chromium (see Dockerfile.v2).
RUN python3 /desktop_supervisor.py install-browser
VOLUME ["/desktops"]
