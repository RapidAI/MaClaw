"""Frozen entry point for the desktop-managed esptool sidecar."""

import esptool

# PyInstaller cannot infer JSON files that esptool loads by dynamically built
# paths. Keep an explicit reference so the packaging command's collect-data
# setting remains the sole source of the frozen resources.
_ = esptool


if __name__ == "__main__":
    esptool._main()
