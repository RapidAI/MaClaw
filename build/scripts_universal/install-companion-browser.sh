#!/bin/bash
# Sourced by the desktop pkg postinstall, which runs as root.
# APP_NAME, APP_PATH, and CURRENT_USER are set by that script.
# Detect Chrome, Edge, and Safari. Register only browsers that are installed.
# Do not download a browser. Do not create a Chrome or Edge profile: a new
# directory here would be owned by root and the user could not launch it.

COMPANION_EXT_ID="iidkjocaelnacgnadidhbedomegjndjk"
COMPANION_HOST_NAME="com.maclaw.companion"
COMPANION_CHROME_STORE="https://clients2.google.com/service/update2/crx"
COMPANION_EDGE_STORE="https://edge.microsoft.com/extensionwebstorebase/v1/crx"
BROWSER_EXT_CHROME_UPDATE_URL="${BROWSER_EXT_CHROME_UPDATE_URL:-}"
BROWSER_EXT_EDGE_UPDATE_URL="${BROWSER_EXT_EDGE_UPDATE_URL:-}"
COMPANION_EXE="$APP_PATH/Contents/MacOS/$APP_NAME"
# sed keeps a home path that contains spaces. The first field alone does not.
USER_HOME="$(dscl . -read "/Users/$CURRENT_USER" NFSHomeDirectory 2>/dev/null | sed 's/^NFSHomeDirectory: //')"
[ -n "$USER_HOME" ] || USER_HOME="/Users/$CURRENT_USER"

json_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

companion_profile_ready() {
    profile="$(dirname "$1")"
    if [ -d "$profile" ]; then
        return 0
    fi
    logger "${APP_NAME}: browser plugin: ${profile} does not exist yet, skipping registration"
    return 1
}

write_companion_host() {
    dir="$1"
    companion_profile_ready "$dir" || return 1
    mkdir -p "$dir" || return 1
    exe_json="$(json_escape "$COMPANION_EXE")"
    if ! printf '%s\n' "{\"name\":\"${COMPANION_HOST_NAME}\",\"description\":\"Add the current paper to Companion\",\"path\":\"${exe_json}\",\"type\":\"stdio\",\"allowed_origins\":[\"chrome-extension://${COMPANION_EXT_ID}/\"]}" > "$dir/${COMPANION_HOST_NAME}.json"; then
        logger "${APP_NAME}: browser plugin: host manifest was not written"
        return 1
    fi
    chown "$CURRENT_USER" "$dir" "$dir/${COMPANION_HOST_NAME}.json" 2>/dev/null || true
    # Chrome ignores a host manifest or directory that group or other can write.
    chmod go-w "$dir" "$dir/${COMPANION_HOST_NAME}.json" 2>/dev/null || true
}

write_companion_store() {
    dir="$1"
    url="$2"
    [ -n "$url" ] || return 0
    companion_profile_ready "$dir" || return 1
    mkdir -p "$dir" || return 1
    printf '%s\n' "{\"external_update_url\":\"${url}\"}" > "$dir/${COMPANION_EXT_ID}.json" || return 1
    chown "$CURRENT_USER" "$dir" "$dir/${COMPANION_EXT_ID}.json" 2>/dev/null || true
    chmod go-w "$dir" "$dir/${COMPANION_EXT_ID}.json" 2>/dev/null || true
}

if [ -d "/Applications/Google Chrome.app" ]; then
    chrome_host="$USER_HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts"
    if write_companion_host "$chrome_host"; then
        logger "${APP_NAME}: browser plugin: Google Chrome is installed, registering"
        chrome_url=""
        if [ "$BROWSER_EXT_CHROME_UPDATE_URL" = "$COMPANION_CHROME_STORE" ]; then
            chrome_url="$COMPANION_CHROME_STORE"
        fi
        if [ -n "$chrome_url" ]; then
            if write_companion_store "$USER_HOME/Library/Application Support/Google/Chrome/External Extensions" "$chrome_url"; then
                logger "${APP_NAME}: browser plugin: Chrome will ask to enable the extension"
            else
                logger "${APP_NAME}: browser plugin: Chrome store file was not written, native host only"
            fi
        elif [ -n "$BROWSER_EXT_CHROME_UPDATE_URL" ]; then
            logger "${APP_NAME}: browser plugin: Chrome store URL is not an allowed value, native host only"
        else
            logger "${APP_NAME}: browser plugin: Chrome store URL is not configured, native host only"
        fi
    fi
else
    logger "${APP_NAME}: browser plugin: Google Chrome is not installed, skipping"
fi

if [ -d "/Applications/Microsoft Edge.app" ]; then
    edge_host="$USER_HOME/Library/Application Support/Microsoft Edge/NativeMessagingHosts"
    if write_companion_host "$edge_host"; then
        logger "${APP_NAME}: browser plugin: Microsoft Edge is installed, registering"
        edge_url=""
        if [ "$BROWSER_EXT_EDGE_UPDATE_URL" = "$COMPANION_EDGE_STORE" ]; then
            edge_url="$COMPANION_EDGE_STORE"
        elif [ -z "$BROWSER_EXT_EDGE_UPDATE_URL" ] && [ "$BROWSER_EXT_CHROME_UPDATE_URL" = "$COMPANION_CHROME_STORE" ]; then
            edge_url="$COMPANION_CHROME_STORE"
            logger "${APP_NAME}: browser plugin: Edge uses the Chrome Web Store URL"
        elif [ -n "$BROWSER_EXT_EDGE_UPDATE_URL" ]; then
            logger "${APP_NAME}: browser plugin: Edge store URL is not an allowed value, native host only"
        else
            logger "${APP_NAME}: browser plugin: Edge store URL is not configured, native host only"
        fi
        if [ -n "$edge_url" ]; then
            if write_companion_store "$USER_HOME/Library/Application Support/Microsoft Edge/External Extensions" "$edge_url"; then
                logger "${APP_NAME}: browser plugin: Edge will ask to enable the extension"
            else
                logger "${APP_NAME}: browser plugin: Edge store file was not written, native host only"
            fi
        fi
    fi
else
    logger "${APP_NAME}: browser plugin: Microsoft Edge is not installed, skipping"
fi

if [ -d "/Applications/Safari.app" ]; then
    logger "${APP_NAME}: browser plugin: Safari is installed. This package does not include a Safari extension yet, so nothing was registered."
else
    logger "${APP_NAME}: browser plugin: Safari is not installed, skipping"
fi
