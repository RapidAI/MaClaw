(() => {
  var __defProp = Object.defineProperty;
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };

  // desktop/log.js
  var log_exports = {};
  __export(log_exports, {
    LogDebug: () => LogDebug,
    LogError: () => LogError,
    LogFatal: () => LogFatal,
    LogInfo: () => LogInfo,
    LogLevel: () => LogLevel,
    LogPrint: () => LogPrint,
    LogTrace: () => LogTrace,
    LogWarning: () => LogWarning,
    SetLogLevel: () => SetLogLevel
  });
  function sendLogMessage(level, message) {
    window.WailsInvoke("L" + level + message);
  }
  function LogTrace(message) {
    sendLogMessage("T", message);
  }
  function LogPrint(message) {
    sendLogMessage("P", message);
  }
  function LogDebug(message) {
    sendLogMessage("D", message);
  }
  function LogInfo(message) {
    sendLogMessage("I", message);
  }
  function LogWarning(message) {
    sendLogMessage("W", message);
  }
  function LogError(message) {
    sendLogMessage("E", message);
  }
  function LogFatal(message) {
    sendLogMessage("F", message);
  }
  function SetLogLevel(loglevel) {
    sendLogMessage("S", loglevel);
  }
  var LogLevel = {
    TRACE: 1,
    DEBUG: 2,
    INFO: 3,
    WARNING: 4,
    ERROR: 5
  };

  // desktop/events.js
  var Listener = class {
    constructor(eventName, callback, maxCallbacks) {
      this.eventName = eventName;
      this.maxCallbacks = maxCallbacks || -1;
      this.Callback = (data) => {
        callback.apply(null, data);
        if (this.maxCallbacks === -1) {
          return false;
        }
        this.maxCallbacks -= 1;
        return this.maxCallbacks === 0;
      };
    }
  };
  var eventListeners = {};
  function EventsOnMultiple(eventName, callback, maxCallbacks) {
    eventListeners[eventName] = eventListeners[eventName] || [];
    const thisListener = new Listener(eventName, callback, maxCallbacks);
    eventListeners[eventName].push(thisListener);
    return () => listenerOff(thisListener);
  }
  function EventsOn(eventName, callback) {
    return EventsOnMultiple(eventName, callback, -1);
  }
  function EventsOnce(eventName, callback) {
    return EventsOnMultiple(eventName, callback, 1);
  }
  function notifyListeners(eventData) {
    let eventName = eventData.name;
    const newEventListenerList = eventListeners[eventName]?.slice() || [];
    if (newEventListenerList.length) {
      for (let count = newEventListenerList.length - 1; count >= 0; count -= 1) {
        const listener = newEventListenerList[count];
        let data = eventData.data;
        const destroy = listener.Callback(data);
        if (destroy) {
          newEventListenerList.splice(count, 1);
        }
      }
      if (newEventListenerList.length === 0) {
        removeListener(eventName);
      } else {
        eventListeners[eventName] = newEventListenerList;
      }
    }
  }
  function EventsNotify(notifyMessage) {
    let message;
    try {
      message = JSON.parse(notifyMessage);
    } catch (e) {
      const error = "Invalid JSON passed to Notify: " + notifyMessage;
      throw new Error(error);
    }
    notifyListeners(message);
  }
  function EventsEmit(eventName) {
    const payload = {
      name: eventName,
      data: [].slice.apply(arguments).slice(1)
    };
    notifyListeners(payload);
    window.WailsInvoke("EE" + JSON.stringify(payload));
  }
  function removeListener(eventName) {
    delete eventListeners[eventName];
    window.WailsInvoke("EX" + eventName);
  }
  function EventsOff(eventName, ...additionalEventNames) {
    removeListener(eventName);
    if (additionalEventNames.length > 0) {
      additionalEventNames.forEach((eventName2) => {
        removeListener(eventName2);
      });
    }
  }
  function EventsOffAll() {
    const eventNames = Object.keys(eventListeners);
    eventNames.forEach((eventName) => {
      removeListener(eventName);
    });
  }
  function listenerOff(listener) {
    const eventName = listener.eventName;
    if (eventListeners[eventName] === void 0)
      return;
    eventListeners[eventName] = eventListeners[eventName].filter((l) => l !== listener);
    if (eventListeners[eventName].length === 0) {
      removeListener(eventName);
    }
  }

  // desktop/calls.js
  var callbacks = {};
  function cryptoRandom() {
    var array = new Uint32Array(1);
    return window.crypto.getRandomValues(array)[0];
  }
  function basicRandom() {
    return Math.random() * 9007199254740991;
  }
  var randomFunc;
  if (window.crypto) {
    randomFunc = cryptoRandom;
  } else {
    randomFunc = basicRandom;
  }
  function Call(name, args, timeout) {
    if (timeout == null) {
      timeout = 0;
    }
    return new Promise(function(resolve, reject) {
      var callbackID;
      do {
        callbackID = name + "-" + randomFunc();
      } while (callbacks[callbackID]);
      var timeoutHandle;
      if (timeout > 0) {
        timeoutHandle = setTimeout(function() {
          reject(Error("Call to " + name + " timed out. Request ID: " + callbackID));
        }, timeout);
      }
      callbacks[callbackID] = {
        timeoutHandle,
        reject,
        resolve
      };
      try {
        const payload = {
          name,
          args,
          callbackID
        };
        window.WailsInvoke("C" + JSON.stringify(payload));
      } catch (e) {
        console.error(e);
      }
    });
  }
  window.ObfuscatedCall = (id, args, timeout) => {
    if (timeout == null) {
      timeout = 0;
    }
    return new Promise(function(resolve, reject) {
      var callbackID;
      do {
        callbackID = id + "-" + randomFunc();
      } while (callbacks[callbackID]);
      var timeoutHandle;
      if (timeout > 0) {
        timeoutHandle = setTimeout(function() {
          reject(Error("Call to method " + id + " timed out. Request ID: " + callbackID));
        }, timeout);
      }
      callbacks[callbackID] = {
        timeoutHandle,
        reject,
        resolve
      };
      try {
        const payload = {
          id,
          args,
          callbackID
        };
        window.WailsInvoke("c" + JSON.stringify(payload));
      } catch (e) {
        console.error(e);
      }
    });
  };
  function Callback(incomingMessage) {
    let message;
    try {
      message = JSON.parse(incomingMessage);
    } catch (e) {
      const error = `Invalid JSON passed to callback: ${e.message}. Message: ${incomingMessage}`;
      runtime.LogDebug(error);
      throw new Error(error);
    }
    let callbackID = message.callbackid;
    let callbackData = callbacks[callbackID];
    if (!callbackData) {
      const error = `Callback '${callbackID}' not registered!!!`;
      console.error(error);
      throw new Error(error);
    }
    clearTimeout(callbackData.timeoutHandle);
    delete callbacks[callbackID];
    if (message.error) {
      callbackData.reject(message.error);
    } else {
      callbackData.resolve(message.result);
    }
  }

  // desktop/bindings.js
  window.go = {};
  function SetBindings(bindingsMap) {
    try {
      bindingsMap = JSON.parse(bindingsMap);
    } catch (e) {
      console.error(e);
    }
    window.go = window.go || {};
    Object.keys(bindingsMap).forEach((packageName) => {
      window.go[packageName] = window.go[packageName] || {};
      Object.keys(bindingsMap[packageName]).forEach((structName) => {
        window.go[packageName][structName] = window.go[packageName][structName] || {};
        Object.keys(bindingsMap[packageName][structName]).forEach((methodName) => {
          window.go[packageName][structName][methodName] = function() {
            let timeout = 0;
            function dynamic() {
              const args = [].slice.call(arguments);
              return Call([packageName, structName, methodName].join("."), args, timeout);
            }
            dynamic.setTimeout = function(newTimeout) {
              timeout = newTimeout;
            };
            dynamic.getTimeout = function() {
              return timeout;
            };
            return dynamic;
          }();
        });
      });
    });
  }

  // desktop/window.js
  var window_exports = {};
  __export(window_exports, {
    WindowCenter: () => WindowCenter,
    WindowFullscreen: () => WindowFullscreen,
    WindowGetPosition: () => WindowGetPosition,
    WindowGetSize: () => WindowGetSize,
    WindowHide: () => WindowHide,
    WindowIsFullscreen: () => WindowIsFullscreen,
    WindowIsMaximised: () => WindowIsMaximised,
    WindowIsMinimised: () => WindowIsMinimised,
    WindowIsNormal: () => WindowIsNormal,
    WindowMaximise: () => WindowMaximise,
    WindowMinimise: () => WindowMinimise,
    WindowReload: () => WindowReload,
    WindowReloadApp: () => WindowReloadApp,
    WindowSetAlwaysOnTop: () => WindowSetAlwaysOnTop,
    WindowSetBackgroundColour: () => WindowSetBackgroundColour,
    WindowSetDarkTheme: () => WindowSetDarkTheme,
    WindowSetLightTheme: () => WindowSetLightTheme,
    WindowSetMaxSize: () => WindowSetMaxSize,
    WindowSetMinSize: () => WindowSetMinSize,
    WindowSetPosition: () => WindowSetPosition,
    WindowSetSize: () => WindowSetSize,
    WindowSetSystemDefaultTheme: () => WindowSetSystemDefaultTheme,
    WindowSetTitle: () => WindowSetTitle,
    WindowShow: () => WindowShow,
    WindowToggleMaximise: () => WindowToggleMaximise,
    WindowUnfullscreen: () => WindowUnfullscreen,
    WindowUnmaximise: () => WindowUnmaximise,
    WindowUnminimise: () => WindowUnminimise
  });
  function WindowReload() {
    window.location.reload();
  }
  function WindowReloadApp() {
    window.WailsInvoke("WR");
  }
  function WindowSetSystemDefaultTheme() {
    window.WailsInvoke("WASDT");
  }
  function WindowSetLightTheme() {
    window.WailsInvoke("WALT");
  }
  function WindowSetDarkTheme() {
    window.WailsInvoke("WADT");
  }
  function WindowCenter() {
    window.WailsInvoke("Wc");
  }
  function WindowSetTitle(title) {
    window.WailsInvoke("WT" + title);
  }
  function WindowFullscreen() {
    if (window.wails?.suspendResizeCursor) {
      window.wails.suspendResizeCursor();
    } else if (window.wails?.flags) {
      window.wails.flags.enableResize = false;
    }
    window.WailsInvoke("WF");
  }
  function WindowUnfullscreen() {
    window.WailsInvoke("Wf");
  }
  function WindowIsFullscreen() {
    return Call(":wails:WindowIsFullscreen");
  }
  function WindowSetSize(width, height) {
    window.WailsInvoke("Ws:" + width + ":" + height);
  }
  function WindowGetSize() {
    return Call(":wails:WindowGetSize");
  }
  function WindowSetMaxSize(width, height) {
    window.WailsInvoke("WZ:" + width + ":" + height);
  }
  function WindowSetMinSize(width, height) {
    window.WailsInvoke("Wz:" + width + ":" + height);
  }
  function WindowSetAlwaysOnTop(b) {
    window.WailsInvoke("WATP:" + (b ? "1" : "0"));
  }
  function WindowSetPosition(x, y) {
    window.WailsInvoke("Wp:" + x + ":" + y);
  }
  function WindowGetPosition() {
    return Call(":wails:WindowGetPos");
  }
  function WindowHide() {
    window.WailsInvoke("WH");
  }
  function WindowShow() {
    window.WailsInvoke("WS");
  }
  function WindowMaximise() {
    window.WailsInvoke("WM");
  }
  function WindowToggleMaximise() {
    window.WailsInvoke("Wt");
  }
  function WindowUnmaximise() {
    window.WailsInvoke("WU");
  }
  function WindowIsMaximised() {
    return Call(":wails:WindowIsMaximised");
  }
  function WindowMinimise() {
    window.WailsInvoke("Wm");
  }
  function WindowUnminimise() {
    window.WailsInvoke("Wu");
  }
  function WindowIsMinimised() {
    return Call(":wails:WindowIsMinimised");
  }
  function WindowIsNormal() {
    return Call(":wails:WindowIsNormal");
  }
  function WindowSetBackgroundColour(R, G, B, A) {
    let rgba = JSON.stringify({ r: R || 0, g: G || 0, b: B || 0, a: A || 255 });
    window.WailsInvoke("Wr:" + rgba);
  }

  // desktop/screen.js
  var screen_exports = {};
  __export(screen_exports, {
    ScreenGetAll: () => ScreenGetAll
  });
  function ScreenGetAll() {
    return Call(":wails:ScreenGetAll");
  }

  // desktop/browser.js
  var browser_exports = {};
  __export(browser_exports, {
    BrowserOpenURL: () => BrowserOpenURL
  });
  function BrowserOpenURL(url) {
    window.WailsInvoke("BO:" + url);
  }

  // desktop/clipboard.js
  var clipboard_exports = {};
  __export(clipboard_exports, {
    ClipboardGetText: () => ClipboardGetText,
    ClipboardSetText: () => ClipboardSetText
  });
  function ClipboardSetText(text) {
    return Call(":wails:ClipboardSetText", [text]);
  }
  function ClipboardGetText() {
    return Call(":wails:ClipboardGetText");
  }

  // desktop/draganddrop.js
  var draganddrop_exports = {};
  __export(draganddrop_exports, {
    CanResolveFilePaths: () => CanResolveFilePaths,
    OnFileDrop: () => OnFileDrop,
    OnFileDropOff: () => OnFileDropOff,
    ResolveFilePaths: () => ResolveFilePaths
  });
  var flags = {
    registered: false,
    defaultUseDropTarget: true,
    useDropTarget: true,
    nextDeactivate: null,
    nextDeactivateTimeout: null
  };
  var DROP_TARGET_ACTIVE = "wails-drop-target-active";
  function checkStyleDropTarget(style) {
    const cssDropValue = style.getPropertyValue(window.wails.flags.cssDropProperty).trim();
    if (cssDropValue) {
      if (cssDropValue === window.wails.flags.cssDropValue) {
        return true;
      }
      return false;
    }
    return false;
  }
  function onDragOver(e) {
    const isFileDrop = e.dataTransfer.types.includes("Files");
    if (!isFileDrop) {
      return;
    }
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
    if (!window.wails.flags.enableWailsDragAndDrop) {
      return;
    }
    if (!flags.useDropTarget) {
      return;
    }
    const element = e.target;
    if (flags.nextDeactivate)
      flags.nextDeactivate();
    if (!element || !checkStyleDropTarget(getComputedStyle(element))) {
      return;
    }
    let currentElement = element;
    while (currentElement) {
      if (checkStyleDropTarget(getComputedStyle(currentElement))) {
        currentElement.classList.add(DROP_TARGET_ACTIVE);
      }
      currentElement = currentElement.parentElement;
    }
  }
  function onDragLeave(e) {
    const isFileDrop = e.dataTransfer.types.includes("Files");
    if (!isFileDrop) {
      return;
    }
    e.preventDefault();
    if (!window.wails.flags.enableWailsDragAndDrop) {
      return;
    }
    if (!flags.useDropTarget) {
      return;
    }
    if (!e.target || !checkStyleDropTarget(getComputedStyle(e.target))) {
      return null;
    }
    if (flags.nextDeactivate)
      flags.nextDeactivate();
    flags.nextDeactivate = () => {
      Array.from(document.getElementsByClassName(DROP_TARGET_ACTIVE)).forEach((el) => el.classList.remove(DROP_TARGET_ACTIVE));
      flags.nextDeactivate = null;
      if (flags.nextDeactivateTimeout) {
        clearTimeout(flags.nextDeactivateTimeout);
        flags.nextDeactivateTimeout = null;
      }
    };
    flags.nextDeactivateTimeout = setTimeout(() => {
      if (flags.nextDeactivate)
        flags.nextDeactivate();
    }, 50);
  }
  function onDrop(e) {
    const isFileDrop = e.dataTransfer.types.includes("Files");
    if (!isFileDrop) {
      return;
    }
    e.preventDefault();
    if (!window.wails.flags.enableWailsDragAndDrop) {
      return;
    }
    if (CanResolveFilePaths()) {
      let files = [];
      if (e.dataTransfer.items) {
        files = [...e.dataTransfer.items].map((item, i) => {
          if (item.kind === "file") {
            return item.getAsFile();
          }
        });
      } else {
        files = [...e.dataTransfer.files];
      }
      window.runtime.ResolveFilePaths(e.x, e.y, files);
    }
    if (!flags.useDropTarget) {
      return;
    }
    if (flags.nextDeactivate)
      flags.nextDeactivate();
    Array.from(document.getElementsByClassName(DROP_TARGET_ACTIVE)).forEach((el) => el.classList.remove(DROP_TARGET_ACTIVE));
  }
  function CanResolveFilePaths() {
    return window.chrome?.webview?.postMessageWithAdditionalObjects != null;
  }
  function ResolveFilePaths(x, y, files) {
    if (window.chrome?.webview?.postMessageWithAdditionalObjects) {
      chrome.webview.postMessageWithAdditionalObjects(`file:drop:${x}:${y}`, files);
    }
  }
  function OnFileDrop(callback, useDropTarget) {
    if (typeof callback !== "function") {
      console.error("DragAndDropCallback is not a function");
      return;
    }
    if (flags.registered) {
      return;
    }
    flags.registered = true;
    const uDTPT = typeof useDropTarget;
    flags.useDropTarget = uDTPT === "undefined" || uDTPT !== "boolean" ? flags.defaultUseDropTarget : useDropTarget;
    window.addEventListener("dragover", onDragOver);
    window.addEventListener("dragleave", onDragLeave);
    window.addEventListener("drop", onDrop);
    let cb = callback;
    if (flags.useDropTarget) {
      cb = function(x, y, paths) {
        const element = document.elementFromPoint(x, y);
        if (!element || !checkStyleDropTarget(getComputedStyle(element))) {
          return null;
        }
        callback(x, y, paths);
      };
    }
    EventsOn("wails:file-drop", cb);
  }
  function OnFileDropOff() {
    window.removeEventListener("dragover", onDragOver);
    window.removeEventListener("dragleave", onDragLeave);
    window.removeEventListener("drop", onDrop);
    EventsOff("wails:file-drop");
    flags.registered = false;
  }

  // desktop/contextmenu.js
  function processDefaultContextMenu(event) {
    const element = event.target;
    const computedStyle = window.getComputedStyle(element);
    const defaultContextMenuAction = computedStyle.getPropertyValue("--default-contextmenu").trim();
    switch (defaultContextMenuAction) {
      case "show":
        return;
      case "hide":
        event.preventDefault();
        return;
      default:
        if (element.isContentEditable) {
          return;
        }
        const selection = window.getSelection();
        const hasSelection = selection.toString().length > 0;
        if (hasSelection) {
          for (let i = 0; i < selection.rangeCount; i++) {
            const range = selection.getRangeAt(i);
            const rects = range.getClientRects();
            for (let j = 0; j < rects.length; j++) {
              const rect = rects[j];
              if (document.elementFromPoint(rect.left, rect.top) === element) {
                return;
              }
            }
          }
        }
        if (element.tagName === "INPUT" || element.tagName === "TEXTAREA") {
          if (hasSelection || !element.readOnly && !element.disabled) {
            return;
          }
        }
        event.preventDefault();
    }
  }

  // desktop/resize_cursor.js
  var RESIZE_CURSORS = {
    se: "se-resize",
    sw: "sw-resize",
    nw: "nw-resize",
    ne: "ne-resize",
    w: "w-resize",
    n: "n-resize",
    s: "s-resize",
    e: "e-resize"
  };
  var RESIZE_METRIC_SLACK = 32;
  function isResizeCursor(cursor) {
    return typeof cursor === "string" && /resize$/.test(cursor);
  }
  function resizeCursorForPointer(outerWidth, outerHeight, innerWidth, innerHeight, x, y, border) {
    if (!(outerWidth > 0) || !(outerHeight > 0))
      return "";
    if (innerWidth - outerWidth > RESIZE_METRIC_SLACK || innerHeight - outerHeight > RESIZE_METRIC_SLACK)
      return "";
    if (x > outerWidth || y > outerHeight)
      return "";
    const right = outerWidth - x < border;
    const left = x < border;
    const top = y < border;
    const bottom = outerHeight - y < border;
    if (right && bottom)
      return RESIZE_CURSORS.se;
    if (left && bottom)
      return RESIZE_CURSORS.sw;
    if (left && top)
      return RESIZE_CURSORS.nw;
    if (top && right)
      return RESIZE_CURSORS.ne;
    if (left)
      return RESIZE_CURSORS.w;
    if (top)
      return RESIZE_CURSORS.n;
    if (bottom)
      return RESIZE_CURSORS.s;
    if (right)
      return RESIZE_CURSORS.e;
    return "";
  }

  // desktop/main.js
  function Quit() {
    window.WailsInvoke("Q");
  }
  function Show() {
    window.WailsInvoke("S");
  }
  function Hide() {
    window.WailsInvoke("H");
  }
  function Environment() {
    return Call(":wails:Environment");
  }
  window.runtime = {
    ...log_exports,
    ...window_exports,
    ...browser_exports,
    ...screen_exports,
    ...clipboard_exports,
    ...draganddrop_exports,
    EventsOn,
    EventsOnce,
    EventsOnMultiple,
    EventsEmit,
    EventsOff,
    EventsOffAll,
    Environment,
    Show,
    Hide,
    Quit
  };
  window.wails = {
    Callback,
    EventsNotify,
    SetBindings,
    eventListeners,
    callbacks,
    flags: {
      disableScrollbarDrag: false,
      disableDefaultContextMenu: false,
      enableResize: false,
      defaultCursor: null,
      borderThickness: 6,
      shouldDrag: false,
      deferDragToMouseMove: true,
      cssDragProperty: "--wails-draggable",
      cssDragValue: "drag",
      cssDropProperty: "--wails-drop-target",
      cssDropValue: "drop",
      enableWailsDragAndDrop: false
    }
  };
  if (window.wailsbindings) {
    window.wails.SetBindings(window.wailsbindings);
    delete window.wails.SetBindings;
  }
  if (false) {
    delete window.wailsbindings;
  }
  var dragTest = function(e) {
    var val = window.getComputedStyle(e.target).getPropertyValue(window.wails.flags.cssDragProperty);
    if (val) {
      val = val.trim();
    }
    if (val !== window.wails.flags.cssDragValue) {
      return false;
    }
    if (e.buttons !== 1) {
      return false;
    }
    if (e.detail !== 1) {
      return false;
    }
    return true;
  };
  window.wails.setCSSDragProperties = function(property, value) {
    window.wails.flags.cssDragProperty = property;
    window.wails.flags.cssDragValue = value;
  };
  window.wails.setCSSDropProperties = function(property, value) {
    window.wails.flags.cssDropProperty = property;
    window.wails.flags.cssDropValue = value;
  };
  window.addEventListener("mousedown", (e) => {
    if (window.wails.flags.resizeEdge) {
      window.WailsInvoke("resize:" + window.wails.flags.resizeEdge);
      e.preventDefault();
      return;
    }
    if (dragTest(e)) {
      if (window.wails.flags.disableScrollbarDrag) {
        if (e.offsetX > e.target.clientWidth || e.offsetY > e.target.clientHeight) {
          return;
        }
      }
      if (window.wails.flags.deferDragToMouseMove) {
        window.wails.flags.shouldDrag = true;
      } else {
        e.preventDefault();
        window.WailsInvoke("drag");
      }
      return;
    } else {
      window.wails.flags.shouldDrag = false;
    }
  });
  window.addEventListener("mouseup", () => {
    window.wails.flags.shouldDrag = false;
  });
  function restoredCursor() {
    const fallback = window.wails.flags.defaultCursor;
    return fallback && !isResizeCursor(fallback) ? fallback : "";
  }
  function setResize(cursor) {
    if (window.wails.flags.resizeEdge === cursor)
      return;
    document.documentElement.style.cursor = cursor || restoredCursor();
    window.wails.flags.resizeEdge = cursor;
  }
  function clearResizeCursor() {
    const current = document.documentElement.style.cursor || "";
    if (!window.wails.flags.resizeEdge && !isResizeCursor(current))
      return;
    document.documentElement.style.cursor = restoredCursor();
    window.wails.flags.resizeEdge = void 0;
  }
  window.wails.suspendResizeCursor = function() {
    window.wails.flags.enableResize = false;
    clearResizeCursor();
  };
  window.addEventListener("mousemove", function(e) {
    if (window.wails.flags.shouldDrag) {
      window.wails.flags.shouldDrag = false;
      let mousePressed = e.buttons !== void 0 ? e.buttons : e.which;
      if (mousePressed > 0) {
        window.WailsInvoke("drag");
        return;
      }
    }
    if (!window.wails.flags.enableResize) {
      if (window.wails.flags.resizeEdge !== void 0)
        clearResizeCursor();
      return;
    }
    if (window.wails.flags.defaultCursor == null) {
      const current = document.documentElement.style.cursor || "";
      window.wails.flags.defaultCursor = isResizeCursor(current) ? "" : current;
    }
    const cursor = resizeCursorForPointer(
      window.outerWidth,
      window.outerHeight,
      window.innerWidth,
      window.innerHeight,
      e.clientX,
      e.clientY,
      window.wails.flags.borderThickness
    );
    if (!cursor) {
      if (window.wails.flags.resizeEdge !== void 0)
        clearResizeCursor();
      return;
    }
    setResize(cursor);
  });
  window.addEventListener("contextmenu", function(e) {
    if (true)
      return;
    if (window.wails.flags.disableDefaultContextMenu) {
      e.preventDefault();
    } else {
      processDefaultContextMenu(e);
    }
  });
  window.WailsInvoke("runtime:ready");
})();
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsiZGVza3RvcC9sb2cuanMiLCAiZGVza3RvcC9ldmVudHMuanMiLCAiZGVza3RvcC9jYWxscy5qcyIsICJkZXNrdG9wL2JpbmRpbmdzLmpzIiwgImRlc2t0b3Avd2luZG93LmpzIiwgImRlc2t0b3Avc2NyZWVuLmpzIiwgImRlc2t0b3AvYnJvd3Nlci5qcyIsICJkZXNrdG9wL2NsaXBib2FyZC5qcyIsICJkZXNrdG9wL2RyYWdhbmRkcm9wLmpzIiwgImRlc2t0b3AvY29udGV4dG1lbnUuanMiLCAiZGVza3RvcC9yZXNpemVfY3Vyc29yLmpzIiwgImRlc2t0b3AvbWFpbi5qcyJdLAogICJzb3VyY2VzQ29udGVudCI6IFsiLypcbiBfICAgICAgIF9fICAgICAgXyBfX1xufCB8ICAgICAvIC9fX18gXyhfKSAvX19fX1xufCB8IC98IC8gLyBfXyBgLyAvIC8gX19fL1xufCB8LyB8LyAvIC9fLyAvIC8gKF9fICApXG58X18vfF9fL1xcX18sXy9fL18vX19fXy9cblRoZSBlbGVjdHJvbiBhbHRlcm5hdGl2ZSBmb3IgR29cbihjKSBMZWEgQW50aG9ueSAyMDE5LXByZXNlbnRcbiovXG5cbi8qIGpzaGludCBlc3ZlcnNpb246IDYgKi9cblxuLyoqXG4gKiBTZW5kcyBhIGxvZyBtZXNzYWdlIHRvIHRoZSBiYWNrZW5kIHdpdGggdGhlIGdpdmVuIGxldmVsICsgbWVzc2FnZVxuICpcbiAqIEBwYXJhbSB7c3RyaW5nfSBsZXZlbFxuICogQHBhcmFtIHtzdHJpbmd9IG1lc3NhZ2VcbiAqL1xuZnVuY3Rpb24gc2VuZExvZ01lc3NhZ2UobGV2ZWwsIG1lc3NhZ2UpIHtcblxuXHQvLyBMb2cgTWVzc2FnZSBmb3JtYXQ6XG5cdC8vIGxbdHlwZV1bbWVzc2FnZV1cblx0d2luZG93LldhaWxzSW52b2tlKCdMJyArIGxldmVsICsgbWVzc2FnZSk7XG59XG5cbi8qKlxuICogTG9nIHRoZSBnaXZlbiB0cmFjZSBtZXNzYWdlIHdpdGggdGhlIGJhY2tlbmRcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gbWVzc2FnZVxuICovXG5leHBvcnQgZnVuY3Rpb24gTG9nVHJhY2UobWVzc2FnZSkge1xuXHRzZW5kTG9nTWVzc2FnZSgnVCcsIG1lc3NhZ2UpO1xufVxuXG4vKipcbiAqIExvZyB0aGUgZ2l2ZW4gbWVzc2FnZSB3aXRoIHRoZSBiYWNrZW5kXG4gKlxuICogQGV4cG9ydFxuICogQHBhcmFtIHtzdHJpbmd9IG1lc3NhZ2VcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIExvZ1ByaW50KG1lc3NhZ2UpIHtcblx0c2VuZExvZ01lc3NhZ2UoJ1AnLCBtZXNzYWdlKTtcbn1cblxuLyoqXG4gKiBMb2cgdGhlIGdpdmVuIGRlYnVnIG1lc3NhZ2Ugd2l0aCB0aGUgYmFja2VuZFxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7c3RyaW5nfSBtZXNzYWdlXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBMb2dEZWJ1ZyhtZXNzYWdlKSB7XG5cdHNlbmRMb2dNZXNzYWdlKCdEJywgbWVzc2FnZSk7XG59XG5cbi8qKlxuICogTG9nIHRoZSBnaXZlbiBpbmZvIG1lc3NhZ2Ugd2l0aCB0aGUgYmFja2VuZFxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7c3RyaW5nfSBtZXNzYWdlXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBMb2dJbmZvKG1lc3NhZ2UpIHtcblx0c2VuZExvZ01lc3NhZ2UoJ0knLCBtZXNzYWdlKTtcbn1cblxuLyoqXG4gKiBMb2cgdGhlIGdpdmVuIHdhcm5pbmcgbWVzc2FnZSB3aXRoIHRoZSBiYWNrZW5kXG4gKlxuICogQGV4cG9ydFxuICogQHBhcmFtIHtzdHJpbmd9IG1lc3NhZ2VcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIExvZ1dhcm5pbmcobWVzc2FnZSkge1xuXHRzZW5kTG9nTWVzc2FnZSgnVycsIG1lc3NhZ2UpO1xufVxuXG4vKipcbiAqIExvZyB0aGUgZ2l2ZW4gZXJyb3IgbWVzc2FnZSB3aXRoIHRoZSBiYWNrZW5kXG4gKlxuICogQGV4cG9ydFxuICogQHBhcmFtIHtzdHJpbmd9IG1lc3NhZ2VcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIExvZ0Vycm9yKG1lc3NhZ2UpIHtcblx0c2VuZExvZ01lc3NhZ2UoJ0UnLCBtZXNzYWdlKTtcbn1cblxuLyoqXG4gKiBMb2cgdGhlIGdpdmVuIGZhdGFsIG1lc3NhZ2Ugd2l0aCB0aGUgYmFja2VuZFxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7c3RyaW5nfSBtZXNzYWdlXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBMb2dGYXRhbChtZXNzYWdlKSB7XG5cdHNlbmRMb2dNZXNzYWdlKCdGJywgbWVzc2FnZSk7XG59XG5cbi8qKlxuICogU2V0cyB0aGUgTG9nIGxldmVsIHRvIHRoZSBnaXZlbiBsb2cgbGV2ZWxcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge251bWJlcn0gbG9nbGV2ZWxcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFNldExvZ0xldmVsKGxvZ2xldmVsKSB7XG5cdHNlbmRMb2dNZXNzYWdlKCdTJywgbG9nbGV2ZWwpO1xufVxuXG4vLyBMb2cgbGV2ZWxzXG5leHBvcnQgY29uc3QgTG9nTGV2ZWwgPSB7XG5cdFRSQUNFOiAxLFxuXHRERUJVRzogMixcblx0SU5GTzogMyxcblx0V0FSTklORzogNCxcblx0RVJST1I6IDUsXG59O1xuIiwgIi8qXG4gXyAgICAgICBfXyAgICAgIF8gX19cbnwgfCAgICAgLyAvX19fIF8oXykgL19fX19cbnwgfCAvfCAvIC8gX18gYC8gLyAvIF9fXy9cbnwgfC8gfC8gLyAvXy8gLyAvIChfXyAgKVxufF9fL3xfXy9cXF9fLF8vXy9fL19fX18vXG5UaGUgZWxlY3Ryb24gYWx0ZXJuYXRpdmUgZm9yIEdvXG4oYykgTGVhIEFudGhvbnkgMjAxOS1wcmVzZW50XG4qL1xuLyoganNoaW50IGVzdmVyc2lvbjogNiAqL1xuXG4vLyBEZWZpbmVzIGEgc2luZ2xlIGxpc3RlbmVyIHdpdGggYSBtYXhpbXVtIG51bWJlciBvZiB0aW1lcyB0byBjYWxsYmFja1xuXG4vKipcbiAqIFRoZSBMaXN0ZW5lciBjbGFzcyBkZWZpbmVzIGEgbGlzdGVuZXIhIDotKVxuICpcbiAqIEBjbGFzcyBMaXN0ZW5lclxuICovXG5jbGFzcyBMaXN0ZW5lciB7XG4gICAgLyoqXG4gICAgICogQ3JlYXRlcyBhbiBpbnN0YW5jZSBvZiBMaXN0ZW5lci5cbiAgICAgKiBAcGFyYW0ge3N0cmluZ30gZXZlbnROYW1lXG4gICAgICogQHBhcmFtIHtmdW5jdGlvbn0gY2FsbGJhY2tcbiAgICAgKiBAcGFyYW0ge251bWJlcn0gbWF4Q2FsbGJhY2tzXG4gICAgICogQG1lbWJlcm9mIExpc3RlbmVyXG4gICAgICovXG4gICAgY29uc3RydWN0b3IoZXZlbnROYW1lLCBjYWxsYmFjaywgbWF4Q2FsbGJhY2tzKSB7XG4gICAgICAgIHRoaXMuZXZlbnROYW1lID0gZXZlbnROYW1lO1xuICAgICAgICAvLyBEZWZhdWx0IG9mIC0xIG1lYW5zIGluZmluaXRlXG4gICAgICAgIHRoaXMubWF4Q2FsbGJhY2tzID0gbWF4Q2FsbGJhY2tzIHx8IC0xO1xuICAgICAgICAvLyBDYWxsYmFjayBpbnZva2VzIHRoZSBjYWxsYmFjayB3aXRoIHRoZSBnaXZlbiBkYXRhXG4gICAgICAgIC8vIFJldHVybnMgdHJ1ZSBpZiB0aGlzIGxpc3RlbmVyIHNob3VsZCBiZSBkZXN0cm95ZWRcbiAgICAgICAgdGhpcy5DYWxsYmFjayA9IChkYXRhKSA9PiB7XG4gICAgICAgICAgICBjYWxsYmFjay5hcHBseShudWxsLCBkYXRhKTtcbiAgICAgICAgICAgIC8vIElmIG1heENhbGxiYWNrcyBpcyBpbmZpbml0ZSwgcmV0dXJuIGZhbHNlIChkbyBub3QgZGVzdHJveSlcbiAgICAgICAgICAgIGlmICh0aGlzLm1heENhbGxiYWNrcyA9PT0gLTEpIHtcbiAgICAgICAgICAgICAgICByZXR1cm4gZmFsc2U7XG4gICAgICAgICAgICB9XG4gICAgICAgICAgICAvLyBEZWNyZW1lbnQgbWF4Q2FsbGJhY2tzLiBSZXR1cm4gdHJ1ZSBpZiBub3cgMCwgb3RoZXJ3aXNlIGZhbHNlXG4gICAgICAgICAgICB0aGlzLm1heENhbGxiYWNrcyAtPSAxO1xuICAgICAgICAgICAgcmV0dXJuIHRoaXMubWF4Q2FsbGJhY2tzID09PSAwO1xuICAgICAgICB9O1xuICAgIH1cbn1cblxuZXhwb3J0IGNvbnN0IGV2ZW50TGlzdGVuZXJzID0ge307XG5cbi8qKlxuICogUmVnaXN0ZXJzIGFuIGV2ZW50IGxpc3RlbmVyIHRoYXQgd2lsbCBiZSBpbnZva2VkIGBtYXhDYWxsYmFja3NgIHRpbWVzIGJlZm9yZSBiZWluZyBkZXN0cm95ZWRcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gZXZlbnROYW1lXG4gKiBAcGFyYW0ge2Z1bmN0aW9ufSBjYWxsYmFja1xuICogQHBhcmFtIHtudW1iZXJ9IG1heENhbGxiYWNrc1xuICogQHJldHVybnMge2Z1bmN0aW9ufSBBIGZ1bmN0aW9uIHRvIGNhbmNlbCB0aGUgbGlzdGVuZXJcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c09uTXVsdGlwbGUoZXZlbnROYW1lLCBjYWxsYmFjaywgbWF4Q2FsbGJhY2tzKSB7XG4gICAgZXZlbnRMaXN0ZW5lcnNbZXZlbnROYW1lXSA9IGV2ZW50TGlzdGVuZXJzW2V2ZW50TmFtZV0gfHwgW107XG4gICAgY29uc3QgdGhpc0xpc3RlbmVyID0gbmV3IExpc3RlbmVyKGV2ZW50TmFtZSwgY2FsbGJhY2ssIG1heENhbGxiYWNrcyk7XG4gICAgZXZlbnRMaXN0ZW5lcnNbZXZlbnROYW1lXS5wdXNoKHRoaXNMaXN0ZW5lcik7XG4gICAgcmV0dXJuICgpID0+IGxpc3RlbmVyT2ZmKHRoaXNMaXN0ZW5lcik7XG59XG5cbi8qKlxuICogUmVnaXN0ZXJzIGFuIGV2ZW50IGxpc3RlbmVyIHRoYXQgd2lsbCBiZSBpbnZva2VkIGV2ZXJ5IHRpbWUgdGhlIGV2ZW50IGlzIGVtaXR0ZWRcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gZXZlbnROYW1lXG4gKiBAcGFyYW0ge2Z1bmN0aW9ufSBjYWxsYmFja1xuICogQHJldHVybnMge2Z1bmN0aW9ufSBBIGZ1bmN0aW9uIHRvIGNhbmNlbCB0aGUgbGlzdGVuZXJcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c09uKGV2ZW50TmFtZSwgY2FsbGJhY2spIHtcbiAgICByZXR1cm4gRXZlbnRzT25NdWx0aXBsZShldmVudE5hbWUsIGNhbGxiYWNrLCAtMSk7XG59XG5cbi8qKlxuICogUmVnaXN0ZXJzIGFuIGV2ZW50IGxpc3RlbmVyIHRoYXQgd2lsbCBiZSBpbnZva2VkIG9uY2UgdGhlbiBkZXN0cm95ZWRcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gZXZlbnROYW1lXG4gKiBAcGFyYW0ge2Z1bmN0aW9ufSBjYWxsYmFja1xuICogQHJldHVybnMge2Z1bmN0aW9ufSBBIGZ1bmN0aW9uIHRvIGNhbmNlbCB0aGUgbGlzdGVuZXJcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c09uY2UoZXZlbnROYW1lLCBjYWxsYmFjaykge1xuICAgIHJldHVybiBFdmVudHNPbk11bHRpcGxlKGV2ZW50TmFtZSwgY2FsbGJhY2ssIDEpO1xufVxuXG5mdW5jdGlvbiBub3RpZnlMaXN0ZW5lcnMoZXZlbnREYXRhKSB7XG5cbiAgICAvLyBHZXQgdGhlIGV2ZW50IG5hbWVcbiAgICBsZXQgZXZlbnROYW1lID0gZXZlbnREYXRhLm5hbWU7XG5cbiAgICAvLyBLZWVwIGEgbGlzdCBvZiBsaXN0ZW5lciBpbmRleGVzIHRvIGRlc3Ryb3lcbiAgICBjb25zdCBuZXdFdmVudExpc3RlbmVyTGlzdCA9IGV2ZW50TGlzdGVuZXJzW2V2ZW50TmFtZV0/LnNsaWNlKCkgfHwgW107XG5cbiAgICAvLyBDaGVjayBpZiB3ZSBoYXZlIGFueSBsaXN0ZW5lcnMgZm9yIHRoaXMgZXZlbnRcbiAgICBpZiAobmV3RXZlbnRMaXN0ZW5lckxpc3QubGVuZ3RoKSB7XG5cbiAgICAgICAgLy8gSXRlcmF0ZSBsaXN0ZW5lcnNcbiAgICAgICAgZm9yIChsZXQgY291bnQgPSBuZXdFdmVudExpc3RlbmVyTGlzdC5sZW5ndGggLSAxOyBjb3VudCA+PSAwOyBjb3VudCAtPSAxKSB7XG5cbiAgICAgICAgICAgIC8vIEdldCBuZXh0IGxpc3RlbmVyXG4gICAgICAgICAgICBjb25zdCBsaXN0ZW5lciA9IG5ld0V2ZW50TGlzdGVuZXJMaXN0W2NvdW50XTtcblxuICAgICAgICAgICAgbGV0IGRhdGEgPSBldmVudERhdGEuZGF0YTtcblxuICAgICAgICAgICAgLy8gRG8gdGhlIGNhbGxiYWNrXG4gICAgICAgICAgICBjb25zdCBkZXN0cm95ID0gbGlzdGVuZXIuQ2FsbGJhY2soZGF0YSk7XG4gICAgICAgICAgICBpZiAoZGVzdHJveSkge1xuICAgICAgICAgICAgICAgIC8vIGlmIHRoZSBsaXN0ZW5lciBpbmRpY2F0ZWQgdG8gZGVzdHJveSBpdHNlbGYsIGFkZCBpdCB0byB0aGUgZGVzdHJveSBsaXN0XG4gICAgICAgICAgICAgICAgbmV3RXZlbnRMaXN0ZW5lckxpc3Quc3BsaWNlKGNvdW50LCAxKTtcbiAgICAgICAgICAgIH1cbiAgICAgICAgfVxuXG4gICAgICAgIC8vIFVwZGF0ZSBjYWxsYmFja3Mgd2l0aCBuZXcgbGlzdCBvZiBsaXN0ZW5lcnNcbiAgICAgICAgaWYgKG5ld0V2ZW50TGlzdGVuZXJMaXN0Lmxlbmd0aCA9PT0gMCkge1xuICAgICAgICAgICAgcmVtb3ZlTGlzdGVuZXIoZXZlbnROYW1lKTtcbiAgICAgICAgfSBlbHNlIHtcbiAgICAgICAgICAgIGV2ZW50TGlzdGVuZXJzW2V2ZW50TmFtZV0gPSBuZXdFdmVudExpc3RlbmVyTGlzdDtcbiAgICAgICAgfVxuICAgIH1cbn1cblxuLyoqXG4gKiBOb3RpZnkgaW5mb3JtcyBmcm9udGVuZCBsaXN0ZW5lcnMgdGhhdCBhbiBldmVudCB3YXMgZW1pdHRlZCB3aXRoIHRoZSBnaXZlbiBkYXRhXG4gKlxuICogQGV4cG9ydFxuICogQHBhcmFtIHtzdHJpbmd9IG5vdGlmeU1lc3NhZ2UgLSBlbmNvZGVkIG5vdGlmaWNhdGlvbiBtZXNzYWdlXG5cbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c05vdGlmeShub3RpZnlNZXNzYWdlKSB7XG4gICAgLy8gUGFyc2UgdGhlIG1lc3NhZ2VcbiAgICBsZXQgbWVzc2FnZTtcbiAgICB0cnkge1xuICAgICAgICBtZXNzYWdlID0gSlNPTi5wYXJzZShub3RpZnlNZXNzYWdlKTtcbiAgICB9IGNhdGNoIChlKSB7XG4gICAgICAgIGNvbnN0IGVycm9yID0gJ0ludmFsaWQgSlNPTiBwYXNzZWQgdG8gTm90aWZ5OiAnICsgbm90aWZ5TWVzc2FnZTtcbiAgICAgICAgdGhyb3cgbmV3IEVycm9yKGVycm9yKTtcbiAgICB9XG4gICAgbm90aWZ5TGlzdGVuZXJzKG1lc3NhZ2UpO1xufVxuXG4vKipcbiAqIEVtaXQgYW4gZXZlbnQgd2l0aCB0aGUgZ2l2ZW4gbmFtZSBhbmQgZGF0YVxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7c3RyaW5nfSBldmVudE5hbWVcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c0VtaXQoZXZlbnROYW1lKSB7XG5cbiAgICBjb25zdCBwYXlsb2FkID0ge1xuICAgICAgICBuYW1lOiBldmVudE5hbWUsXG4gICAgICAgIGRhdGE6IFtdLnNsaWNlLmFwcGx5KGFyZ3VtZW50cykuc2xpY2UoMSksXG4gICAgfTtcblxuICAgIC8vIE5vdGlmeSBKUyBsaXN0ZW5lcnNcbiAgICBub3RpZnlMaXN0ZW5lcnMocGF5bG9hZCk7XG5cbiAgICAvLyBOb3RpZnkgR28gbGlzdGVuZXJzXG4gICAgd2luZG93LldhaWxzSW52b2tlKCdFRScgKyBKU09OLnN0cmluZ2lmeShwYXlsb2FkKSk7XG59XG5cbmZ1bmN0aW9uIHJlbW92ZUxpc3RlbmVyKGV2ZW50TmFtZSkge1xuICAgIC8vIFJlbW92ZSBsb2NhbCBsaXN0ZW5lcnNcbiAgICBkZWxldGUgZXZlbnRMaXN0ZW5lcnNbZXZlbnROYW1lXTtcblxuICAgIC8vIE5vdGlmeSBHbyBsaXN0ZW5lcnNcbiAgICB3aW5kb3cuV2FpbHNJbnZva2UoJ0VYJyArIGV2ZW50TmFtZSk7XG59XG5cbi8qKlxuICogT2ZmIHVucmVnaXN0ZXJzIGEgbGlzdGVuZXIgcHJldmlvdXNseSByZWdpc3RlcmVkIHdpdGggT24sXG4gKiBvcHRpb25hbGx5IG11bHRpcGxlIGxpc3RlbmVyZXMgY2FuIGJlIHVucmVnaXN0ZXJlZCB2aWEgYGFkZGl0aW9uYWxFdmVudE5hbWVzYFxuICpcbiAqIEBwYXJhbSB7c3RyaW5nfSBldmVudE5hbWVcbiAqIEBwYXJhbSAgey4uLnN0cmluZ30gYWRkaXRpb25hbEV2ZW50TmFtZXNcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c09mZihldmVudE5hbWUsIC4uLmFkZGl0aW9uYWxFdmVudE5hbWVzKSB7XG4gICAgcmVtb3ZlTGlzdGVuZXIoZXZlbnROYW1lKVxuXG4gICAgaWYgKGFkZGl0aW9uYWxFdmVudE5hbWVzLmxlbmd0aCA+IDApIHtcbiAgICAgICAgYWRkaXRpb25hbEV2ZW50TmFtZXMuZm9yRWFjaChldmVudE5hbWUgPT4ge1xuICAgICAgICAgICAgcmVtb3ZlTGlzdGVuZXIoZXZlbnROYW1lKVxuICAgICAgICB9KVxuICAgIH1cbn1cblxuLyoqXG4gKiBPZmYgdW5yZWdpc3RlcnMgYWxsIGV2ZW50IGxpc3RlbmVycyBwcmV2aW91c2x5IHJlZ2lzdGVyZWQgd2l0aCBPblxuICovXG4gZXhwb3J0IGZ1bmN0aW9uIEV2ZW50c09mZkFsbCgpIHtcbiAgICBjb25zdCBldmVudE5hbWVzID0gT2JqZWN0LmtleXMoZXZlbnRMaXN0ZW5lcnMpO1xuICAgIGV2ZW50TmFtZXMuZm9yRWFjaChldmVudE5hbWUgPT4ge1xuICAgICAgICByZW1vdmVMaXN0ZW5lcihldmVudE5hbWUpXG4gICAgfSlcbn1cblxuLyoqXG4gKiBsaXN0ZW5lck9mZiB1bnJlZ2lzdGVycyBhIGxpc3RlbmVyIHByZXZpb3VzbHkgcmVnaXN0ZXJlZCB3aXRoIEV2ZW50c09uXG4gKlxuICogQHBhcmFtIHtMaXN0ZW5lcn0gbGlzdGVuZXJcbiAqL1xuIGZ1bmN0aW9uIGxpc3RlbmVyT2ZmKGxpc3RlbmVyKSB7XG4gICAgY29uc3QgZXZlbnROYW1lID0gbGlzdGVuZXIuZXZlbnROYW1lO1xuICAgIGlmIChldmVudExpc3RlbmVyc1tldmVudE5hbWVdID09PSB1bmRlZmluZWQpIHJldHVybjtcblxuICAgIC8vIFJlbW92ZSBsb2NhbCBsaXN0ZW5lclxuICAgIGV2ZW50TGlzdGVuZXJzW2V2ZW50TmFtZV0gPSBldmVudExpc3RlbmVyc1tldmVudE5hbWVdLmZpbHRlcihsID0+IGwgIT09IGxpc3RlbmVyKTtcblxuICAgIC8vIENsZWFuIHVwIGlmIHRoZXJlIGFyZSBubyBldmVudCBsaXN0ZW5lcnMgbGVmdFxuICAgIGlmIChldmVudExpc3RlbmVyc1tldmVudE5hbWVdLmxlbmd0aCA9PT0gMCkge1xuICAgICAgICByZW1vdmVMaXN0ZW5lcihldmVudE5hbWUpO1xuICAgIH1cbn1cbiIsICIvKlxuIF8gICAgICAgX18gICAgICBfIF9fXG58IHwgICAgIC8gL19fXyBfKF8pIC9fX19fXG58IHwgL3wgLyAvIF9fIGAvIC8gLyBfX18vXG58IHwvIHwvIC8gL18vIC8gLyAoX18gIClcbnxfXy98X18vXFxfXyxfL18vXy9fX19fL1xuVGhlIGVsZWN0cm9uIGFsdGVybmF0aXZlIGZvciBHb1xuKGMpIExlYSBBbnRob255IDIwMTktcHJlc2VudFxuKi9cbi8qIGpzaGludCBlc3ZlcnNpb246IDYgKi9cblxuZXhwb3J0IGNvbnN0IGNhbGxiYWNrcyA9IHt9O1xuXG4vKipcbiAqIFJldHVybnMgYSBudW1iZXIgZnJvbSB0aGUgbmF0aXZlIGJyb3dzZXIgcmFuZG9tIGZ1bmN0aW9uXG4gKlxuICogQHJldHVybnMgbnVtYmVyXG4gKi9cbmZ1bmN0aW9uIGNyeXB0b1JhbmRvbSgpIHtcblx0dmFyIGFycmF5ID0gbmV3IFVpbnQzMkFycmF5KDEpO1xuXHRyZXR1cm4gd2luZG93LmNyeXB0by5nZXRSYW5kb21WYWx1ZXMoYXJyYXkpWzBdO1xufVxuXG4vKipcbiAqIFJldHVybnMgYSBudW1iZXIgdXNpbmcgZGEgb2xkLXNrb29sIE1hdGguUmFuZG9tXG4gKiBJIGxpa2VzIHRvIGNhbGwgaXQgTE9MUmFuZG9tXG4gKlxuICogQHJldHVybnMgbnVtYmVyXG4gKi9cbmZ1bmN0aW9uIGJhc2ljUmFuZG9tKCkge1xuXHRyZXR1cm4gTWF0aC5yYW5kb20oKSAqIDkwMDcxOTkyNTQ3NDA5OTE7XG59XG5cbi8vIFBpY2sgYSByYW5kb20gbnVtYmVyIGZ1bmN0aW9uIGJhc2VkIG9uIGJyb3dzZXIgY2FwYWJpbGl0eVxudmFyIHJhbmRvbUZ1bmM7XG5pZiAod2luZG93LmNyeXB0bykge1xuXHRyYW5kb21GdW5jID0gY3J5cHRvUmFuZG9tO1xufSBlbHNlIHtcblx0cmFuZG9tRnVuYyA9IGJhc2ljUmFuZG9tO1xufVxuXG5cbi8qKlxuICogQ2FsbCBzZW5kcyBhIG1lc3NhZ2UgdG8gdGhlIGJhY2tlbmQgdG8gY2FsbCB0aGUgYmluZGluZyB3aXRoIHRoZVxuICogZ2l2ZW4gZGF0YS4gQSBwcm9taXNlIGlzIHJldHVybmVkIGFuZCB3aWxsIGJlIGNvbXBsZXRlZCB3aGVuIHRoZVxuICogYmFja2VuZCByZXNwb25kcy4gVGhpcyB3aWxsIGJlIHJlc29sdmVkIHdoZW4gdGhlIGNhbGwgd2FzIHN1Y2Nlc3NmdWxcbiAqIG9yIHJlamVjdGVkIGlmIGFuIGVycm9yIGlzIHBhc3NlZCBiYWNrLlxuICogVGhlcmUgaXMgYSB0aW1lb3V0IG1lY2hhbmlzbS4gSWYgdGhlIGNhbGwgZG9lc24ndCByZXNwb25kIGluIHRoZSBnaXZlblxuICogdGltZSAoaW4gbWlsbGlzZWNvbmRzKSB0aGVuIHRoZSBwcm9taXNlIGlzIHJlamVjdGVkLlxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7c3RyaW5nfSBuYW1lXG4gKiBAcGFyYW0ge2FueT19IGFyZ3NcbiAqIEBwYXJhbSB7bnVtYmVyPX0gdGltZW91dFxuICogQHJldHVybnNcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIENhbGwobmFtZSwgYXJncywgdGltZW91dCkge1xuXG5cdC8vIFRpbWVvdXQgaW5maW5pdGUgYnkgZGVmYXVsdFxuXHRpZiAodGltZW91dCA9PSBudWxsKSB7XG5cdFx0dGltZW91dCA9IDA7XG5cdH1cblxuXHQvLyBDcmVhdGUgYSBwcm9taXNlXG5cdHJldHVybiBuZXcgUHJvbWlzZShmdW5jdGlvbiAocmVzb2x2ZSwgcmVqZWN0KSB7XG5cblx0XHQvLyBDcmVhdGUgYSB1bmlxdWUgY2FsbGJhY2tJRFxuXHRcdHZhciBjYWxsYmFja0lEO1xuXHRcdGRvIHtcblx0XHRcdGNhbGxiYWNrSUQgPSBuYW1lICsgJy0nICsgcmFuZG9tRnVuYygpO1xuXHRcdH0gd2hpbGUgKGNhbGxiYWNrc1tjYWxsYmFja0lEXSk7XG5cblx0XHR2YXIgdGltZW91dEhhbmRsZTtcblx0XHQvLyBTZXQgdGltZW91dFxuXHRcdGlmICh0aW1lb3V0ID4gMCkge1xuXHRcdFx0dGltZW91dEhhbmRsZSA9IHNldFRpbWVvdXQoZnVuY3Rpb24gKCkge1xuXHRcdFx0XHRyZWplY3QoRXJyb3IoJ0NhbGwgdG8gJyArIG5hbWUgKyAnIHRpbWVkIG91dC4gUmVxdWVzdCBJRDogJyArIGNhbGxiYWNrSUQpKTtcblx0XHRcdH0sIHRpbWVvdXQpO1xuXHRcdH1cblxuXHRcdC8vIFN0b3JlIGNhbGxiYWNrXG5cdFx0Y2FsbGJhY2tzW2NhbGxiYWNrSURdID0ge1xuXHRcdFx0dGltZW91dEhhbmRsZTogdGltZW91dEhhbmRsZSxcblx0XHRcdHJlamVjdDogcmVqZWN0LFxuXHRcdFx0cmVzb2x2ZTogcmVzb2x2ZVxuXHRcdH07XG5cblx0XHR0cnkge1xuXHRcdFx0Y29uc3QgcGF5bG9hZCA9IHtcblx0XHRcdFx0bmFtZSxcblx0XHRcdFx0YXJncyxcblx0XHRcdFx0Y2FsbGJhY2tJRCxcblx0XHRcdH07XG5cbiAgICAgICAgICAgIC8vIE1ha2UgdGhlIGNhbGxcbiAgICAgICAgICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnQycgKyBKU09OLnN0cmluZ2lmeShwYXlsb2FkKSk7XG4gICAgICAgIH0gY2F0Y2ggKGUpIHtcbiAgICAgICAgICAgIC8vIGVzbGludC1kaXNhYmxlLW5leHQtbGluZVxuICAgICAgICAgICAgY29uc29sZS5lcnJvcihlKTtcbiAgICAgICAgfVxuICAgIH0pO1xufVxuXG53aW5kb3cuT2JmdXNjYXRlZENhbGwgPSAoaWQsIGFyZ3MsIHRpbWVvdXQpID0+IHtcblxuICAgIC8vIFRpbWVvdXQgaW5maW5pdGUgYnkgZGVmYXVsdFxuICAgIGlmICh0aW1lb3V0ID09IG51bGwpIHtcbiAgICAgICAgdGltZW91dCA9IDA7XG4gICAgfVxuXG4gICAgLy8gQ3JlYXRlIGEgcHJvbWlzZVxuICAgIHJldHVybiBuZXcgUHJvbWlzZShmdW5jdGlvbiAocmVzb2x2ZSwgcmVqZWN0KSB7XG5cbiAgICAgICAgLy8gQ3JlYXRlIGEgdW5pcXVlIGNhbGxiYWNrSURcbiAgICAgICAgdmFyIGNhbGxiYWNrSUQ7XG4gICAgICAgIGRvIHtcbiAgICAgICAgICAgIGNhbGxiYWNrSUQgPSBpZCArICctJyArIHJhbmRvbUZ1bmMoKTtcbiAgICAgICAgfSB3aGlsZSAoY2FsbGJhY2tzW2NhbGxiYWNrSURdKTtcblxuICAgICAgICB2YXIgdGltZW91dEhhbmRsZTtcbiAgICAgICAgLy8gU2V0IHRpbWVvdXRcbiAgICAgICAgaWYgKHRpbWVvdXQgPiAwKSB7XG4gICAgICAgICAgICB0aW1lb3V0SGFuZGxlID0gc2V0VGltZW91dChmdW5jdGlvbiAoKSB7XG4gICAgICAgICAgICAgICAgcmVqZWN0KEVycm9yKCdDYWxsIHRvIG1ldGhvZCAnICsgaWQgKyAnIHRpbWVkIG91dC4gUmVxdWVzdCBJRDogJyArIGNhbGxiYWNrSUQpKTtcbiAgICAgICAgICAgIH0sIHRpbWVvdXQpO1xuICAgICAgICB9XG5cbiAgICAgICAgLy8gU3RvcmUgY2FsbGJhY2tcbiAgICAgICAgY2FsbGJhY2tzW2NhbGxiYWNrSURdID0ge1xuICAgICAgICAgICAgdGltZW91dEhhbmRsZTogdGltZW91dEhhbmRsZSxcbiAgICAgICAgICAgIHJlamVjdDogcmVqZWN0LFxuICAgICAgICAgICAgcmVzb2x2ZTogcmVzb2x2ZVxuICAgICAgICB9O1xuXG4gICAgICAgIHRyeSB7XG4gICAgICAgICAgICBjb25zdCBwYXlsb2FkID0ge1xuXHRcdFx0XHRpZCxcblx0XHRcdFx0YXJncyxcblx0XHRcdFx0Y2FsbGJhY2tJRCxcblx0XHRcdH07XG5cbiAgICAgICAgICAgIC8vIE1ha2UgdGhlIGNhbGxcbiAgICAgICAgICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnYycgKyBKU09OLnN0cmluZ2lmeShwYXlsb2FkKSk7XG4gICAgICAgIH0gY2F0Y2ggKGUpIHtcbiAgICAgICAgICAgIC8vIGVzbGludC1kaXNhYmxlLW5leHQtbGluZVxuICAgICAgICAgICAgY29uc29sZS5lcnJvcihlKTtcbiAgICAgICAgfVxuICAgIH0pO1xufTtcblxuXG4vKipcbiAqIENhbGxlZCBieSB0aGUgYmFja2VuZCB0byByZXR1cm4gZGF0YSB0byBhIHByZXZpb3VzbHkgY2FsbGVkXG4gKiBiaW5kaW5nIGludm9jYXRpb25cbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gaW5jb21pbmdNZXNzYWdlXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBDYWxsYmFjayhpbmNvbWluZ01lc3NhZ2UpIHtcblx0Ly8gUGFyc2UgdGhlIG1lc3NhZ2Vcblx0bGV0IG1lc3NhZ2U7XG5cdHRyeSB7XG5cdFx0bWVzc2FnZSA9IEpTT04ucGFyc2UoaW5jb21pbmdNZXNzYWdlKTtcblx0fSBjYXRjaCAoZSkge1xuXHRcdGNvbnN0IGVycm9yID0gYEludmFsaWQgSlNPTiBwYXNzZWQgdG8gY2FsbGJhY2s6ICR7ZS5tZXNzYWdlfS4gTWVzc2FnZTogJHtpbmNvbWluZ01lc3NhZ2V9YDtcblx0XHRydW50aW1lLkxvZ0RlYnVnKGVycm9yKTtcblx0XHR0aHJvdyBuZXcgRXJyb3IoZXJyb3IpO1xuXHR9XG5cdGxldCBjYWxsYmFja0lEID0gbWVzc2FnZS5jYWxsYmFja2lkO1xuXHRsZXQgY2FsbGJhY2tEYXRhID0gY2FsbGJhY2tzW2NhbGxiYWNrSURdO1xuXHRpZiAoIWNhbGxiYWNrRGF0YSkge1xuXHRcdGNvbnN0IGVycm9yID0gYENhbGxiYWNrICcke2NhbGxiYWNrSUR9JyBub3QgcmVnaXN0ZXJlZCEhIWA7XG5cdFx0Y29uc29sZS5lcnJvcihlcnJvcik7IC8vIGVzbGludC1kaXNhYmxlLWxpbmVcblx0XHR0aHJvdyBuZXcgRXJyb3IoZXJyb3IpO1xuXHR9XG5cdGNsZWFyVGltZW91dChjYWxsYmFja0RhdGEudGltZW91dEhhbmRsZSk7XG5cblx0ZGVsZXRlIGNhbGxiYWNrc1tjYWxsYmFja0lEXTtcblxuXHRpZiAobWVzc2FnZS5lcnJvcikge1xuXHRcdGNhbGxiYWNrRGF0YS5yZWplY3QobWVzc2FnZS5lcnJvcik7XG5cdH0gZWxzZSB7XG5cdFx0Y2FsbGJhY2tEYXRhLnJlc29sdmUobWVzc2FnZS5yZXN1bHQpO1xuXHR9XG59XG4iLCAiLypcbiBfICAgICAgIF9fICAgICAgXyBfXyAgICBcbnwgfCAgICAgLyAvX19fIF8oXykgL19fX19cbnwgfCAvfCAvIC8gX18gYC8gLyAvIF9fXy9cbnwgfC8gfC8gLyAvXy8gLyAvIChfXyAgKSBcbnxfXy98X18vXFxfXyxfL18vXy9fX19fLyAgXG5UaGUgZWxlY3Ryb24gYWx0ZXJuYXRpdmUgZm9yIEdvXG4oYykgTGVhIEFudGhvbnkgMjAxOS1wcmVzZW50XG4qL1xuLyoganNoaW50IGVzdmVyc2lvbjogNiAqL1xuXG5pbXBvcnQge0NhbGx9IGZyb20gJy4vY2FsbHMnO1xuXG4vLyBUaGlzIGlzIHdoZXJlIHdlIGJpbmQgZ28gbWV0aG9kIHdyYXBwZXJzXG53aW5kb3cuZ28gPSB7fTtcblxuZXhwb3J0IGZ1bmN0aW9uIFNldEJpbmRpbmdzKGJpbmRpbmdzTWFwKSB7XG5cdHRyeSB7XG5cdFx0YmluZGluZ3NNYXAgPSBKU09OLnBhcnNlKGJpbmRpbmdzTWFwKTtcblx0fSBjYXRjaCAoZSkge1xuXHRcdGNvbnNvbGUuZXJyb3IoZSk7XG5cdH1cblxuXHQvLyBJbml0aWFsaXNlIHRoZSBiaW5kaW5ncyBtYXBcblx0d2luZG93LmdvID0gd2luZG93LmdvIHx8IHt9O1xuXG5cdC8vIEl0ZXJhdGUgcGFja2FnZSBuYW1lc1xuXHRPYmplY3Qua2V5cyhiaW5kaW5nc01hcCkuZm9yRWFjaCgocGFja2FnZU5hbWUpID0+IHtcblxuXHRcdC8vIENyZWF0ZSBpbm5lciBtYXAgaWYgaXQgZG9lc24ndCBleGlzdFxuXHRcdHdpbmRvdy5nb1twYWNrYWdlTmFtZV0gPSB3aW5kb3cuZ29bcGFja2FnZU5hbWVdIHx8IHt9O1xuXG5cdFx0Ly8gSXRlcmF0ZSBzdHJ1Y3QgbmFtZXNcblx0XHRPYmplY3Qua2V5cyhiaW5kaW5nc01hcFtwYWNrYWdlTmFtZV0pLmZvckVhY2goKHN0cnVjdE5hbWUpID0+IHtcblxuXHRcdFx0Ly8gQ3JlYXRlIGlubmVyIG1hcCBpZiBpdCBkb2Vzbid0IGV4aXN0XG5cdFx0XHR3aW5kb3cuZ29bcGFja2FnZU5hbWVdW3N0cnVjdE5hbWVdID0gd2luZG93LmdvW3BhY2thZ2VOYW1lXVtzdHJ1Y3ROYW1lXSB8fCB7fTtcblxuXHRcdFx0T2JqZWN0LmtleXMoYmluZGluZ3NNYXBbcGFja2FnZU5hbWVdW3N0cnVjdE5hbWVdKS5mb3JFYWNoKChtZXRob2ROYW1lKSA9PiB7XG5cblx0XHRcdFx0d2luZG93LmdvW3BhY2thZ2VOYW1lXVtzdHJ1Y3ROYW1lXVttZXRob2ROYW1lXSA9IGZ1bmN0aW9uICgpIHtcblxuXHRcdFx0XHRcdC8vIE5vIHRpbWVvdXQgYnkgZGVmYXVsdFxuXHRcdFx0XHRcdGxldCB0aW1lb3V0ID0gMDtcblxuXHRcdFx0XHRcdC8vIEFjdHVhbCBmdW5jdGlvblxuXHRcdFx0XHRcdGZ1bmN0aW9uIGR5bmFtaWMoKSB7XG5cdFx0XHRcdFx0XHRjb25zdCBhcmdzID0gW10uc2xpY2UuY2FsbChhcmd1bWVudHMpO1xuXHRcdFx0XHRcdFx0cmV0dXJuIENhbGwoW3BhY2thZ2VOYW1lLCBzdHJ1Y3ROYW1lLCBtZXRob2ROYW1lXS5qb2luKCcuJyksIGFyZ3MsIHRpbWVvdXQpO1xuXHRcdFx0XHRcdH1cblxuXHRcdFx0XHRcdC8vIEFsbG93IHNldHRpbmcgdGltZW91dCB0byBmdW5jdGlvblxuXHRcdFx0XHRcdGR5bmFtaWMuc2V0VGltZW91dCA9IGZ1bmN0aW9uIChuZXdUaW1lb3V0KSB7XG5cdFx0XHRcdFx0XHR0aW1lb3V0ID0gbmV3VGltZW91dDtcblx0XHRcdFx0XHR9O1xuXG5cdFx0XHRcdFx0Ly8gQWxsb3cgZ2V0dGluZyB0aW1lb3V0IHRvIGZ1bmN0aW9uXG5cdFx0XHRcdFx0ZHluYW1pYy5nZXRUaW1lb3V0ID0gZnVuY3Rpb24gKCkge1xuXHRcdFx0XHRcdFx0cmV0dXJuIHRpbWVvdXQ7XG5cdFx0XHRcdFx0fTtcblxuXHRcdFx0XHRcdHJldHVybiBkeW5hbWljO1xuXHRcdFx0XHR9KCk7XG5cdFx0XHR9KTtcblx0XHR9KTtcblx0fSk7XG59XG4iLCAiLypcbiBfXHQgICBfX1x0ICBfIF9fXG58IHxcdCAvIC9fX18gXyhfKSAvX19fX1xufCB8IC98IC8gLyBfXyBgLyAvIC8gX19fL1xufCB8LyB8LyAvIC9fLyAvIC8gKF9fICApXG58X18vfF9fL1xcX18sXy9fL18vX19fXy9cblRoZSBlbGVjdHJvbiBhbHRlcm5hdGl2ZSBmb3IgR29cbihjKSBMZWEgQW50aG9ueSAyMDE5LXByZXNlbnRcbiovXG5cbi8qIGpzaGludCBlc3ZlcnNpb246IDkgKi9cblxuXG5pbXBvcnQge0NhbGx9IGZyb20gXCIuL2NhbGxzXCI7XG5cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dSZWxvYWQoKSB7XG4gICAgd2luZG93LmxvY2F0aW9uLnJlbG9hZCgpO1xufVxuXG5leHBvcnQgZnVuY3Rpb24gV2luZG93UmVsb2FkQXBwKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV1InKTtcbn1cblxuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1NldFN5c3RlbURlZmF1bHRUaGVtZSgpIHtcbiAgICB3aW5kb3cuV2FpbHNJbnZva2UoJ1dBU0RUJyk7XG59XG5cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dTZXRMaWdodFRoZW1lKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV0FMVCcpO1xufVxuXG5leHBvcnQgZnVuY3Rpb24gV2luZG93U2V0RGFya1RoZW1lKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV0FEVCcpO1xufVxuXG4vKipcbiAqIFBsYWNlIHRoZSB3aW5kb3cgaW4gdGhlIGNlbnRlciBvZiB0aGUgc2NyZWVuXG4gKlxuICogQGV4cG9ydFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93Q2VudGVyKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV2MnKTtcbn1cblxuLyoqXG4gKiBTZXRzIHRoZSB3aW5kb3cgdGl0bGVcbiAqXG4gKiBAcGFyYW0ge3N0cmluZ30gdGl0bGVcbiAqIEBleHBvcnRcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1NldFRpdGxlKHRpdGxlKSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXVCcgKyB0aXRsZSk7XG59XG5cbi8qKlxuICogTWFrZXMgdGhlIHdpbmRvdyBnbyBmdWxsc2NyZWVuXG4gKlxuICogQGV4cG9ydFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93RnVsbHNjcmVlbigpIHtcbiAgICAvLyBTdG9wIGZyYW1lbGVzcyBlZGdlIHRyYWNraW5nIGluIHRoaXMgc2FtZSBKUyB0dXJuLCBiZWZvcmUgdGhlIG5hdGl2ZVxuICAgIC8vIHRyYW5zaXRpb24gY2FuIHJlc2l6ZSB0aGUgd2luZG93IHVuZGVyIHRoZSBwb2ludGVyIGFuZCBsYXRjaCBhIHNpemluZ1xuICAgIC8vIGN1cnNvci4gVGhlIEdvIHNpZGUgY2xlYXJzIG9uY2UgbW9yZSBhZnRlciB0aGUgc2l6ZSBjaGFuZ2UuXG4gICAgaWYgKHdpbmRvdy53YWlscz8uc3VzcGVuZFJlc2l6ZUN1cnNvcikge1xuICAgICAgICB3aW5kb3cud2FpbHMuc3VzcGVuZFJlc2l6ZUN1cnNvcigpO1xuICAgIH0gZWxzZSBpZiAod2luZG93LndhaWxzPy5mbGFncykge1xuICAgICAgICB3aW5kb3cud2FpbHMuZmxhZ3MuZW5hYmxlUmVzaXplID0gZmFsc2U7XG4gICAgfVxuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV0YnKTtcbn1cblxuLyoqXG4gKiBSZXZlcnRzIHRoZSB3aW5kb3cgZnJvbSBmdWxsc2NyZWVuXG4gKlxuICogQGV4cG9ydFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93VW5mdWxsc2NyZWVuKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV2YnKTtcbn1cblxuLyoqXG4gKiBSZXR1cm5zIHRoZSBzdGF0ZSBvZiB0aGUgd2luZG93LCBpLmUuIHdoZXRoZXIgdGhlIHdpbmRvdyBpcyBpbiBmdWxsIHNjcmVlbiBtb2RlIG9yIG5vdC5cbiAqXG4gKiBAZXhwb3J0XG4gKiBAcmV0dXJuIHtQcm9taXNlPGJvb2xlYW4+fSBUaGUgc3RhdGUgb2YgdGhlIHdpbmRvd1xuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93SXNGdWxsc2NyZWVuKCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOldpbmRvd0lzRnVsbHNjcmVlblwiKTtcbn1cblxuLyoqXG4gKiBTZXQgdGhlIFNpemUgb2YgdGhlIHdpbmRvd1xuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7bnVtYmVyfSB3aWR0aFxuICogQHBhcmFtIHtudW1iZXJ9IGhlaWdodFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93U2V0U2l6ZSh3aWR0aCwgaGVpZ2h0KSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXczonICsgd2lkdGggKyAnOicgKyBoZWlnaHQpO1xufVxuXG4vKipcbiAqIEdldCB0aGUgU2l6ZSBvZiB0aGUgd2luZG93XG4gKlxuICogQGV4cG9ydFxuICogQHJldHVybiB7UHJvbWlzZTx7dzogbnVtYmVyLCBoOiBudW1iZXJ9Pn0gVGhlIHNpemUgb2YgdGhlIHdpbmRvd1xuXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dHZXRTaXplKCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOldpbmRvd0dldFNpemVcIik7XG59XG5cbi8qKlxuICogU2V0IHRoZSBtYXhpbXVtIHNpemUgb2YgdGhlIHdpbmRvd1xuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7bnVtYmVyfSB3aWR0aFxuICogQHBhcmFtIHtudW1iZXJ9IGhlaWdodFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93U2V0TWF4U2l6ZSh3aWR0aCwgaGVpZ2h0KSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXWjonICsgd2lkdGggKyAnOicgKyBoZWlnaHQpO1xufVxuXG4vKipcbiAqIFNldCB0aGUgbWluaW11bSBzaXplIG9mIHRoZSB3aW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge251bWJlcn0gd2lkdGhcbiAqIEBwYXJhbSB7bnVtYmVyfSBoZWlnaHRcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1NldE1pblNpemUod2lkdGgsIGhlaWdodCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV3o6JyArIHdpZHRoICsgJzonICsgaGVpZ2h0KTtcbn1cblxuXG5cbi8qKlxuICogU2V0IHRoZSB3aW5kb3cgQWx3YXlzT25Ub3Agb3Igbm90IG9uIHRvcFxuICpcbiAqIEBleHBvcnRcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1NldEFsd2F5c09uVG9wKGIpIHtcblxuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV0FUUDonICsgKGIgPyAnMScgOiAnMCcpKTtcbn1cblxuXG5cblxuLyoqXG4gKiBTZXQgdGhlIFBvc2l0aW9uIG9mIHRoZSB3aW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge251bWJlcn0geFxuICogQHBhcmFtIHtudW1iZXJ9IHlcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1NldFBvc2l0aW9uKHgsIHkpIHtcbiAgICB3aW5kb3cuV2FpbHNJbnZva2UoJ1dwOicgKyB4ICsgJzonICsgeSk7XG59XG5cbi8qKlxuICogR2V0IHRoZSBQb3NpdGlvbiBvZiB0aGUgd2luZG93XG4gKlxuICogQGV4cG9ydFxuICogQHJldHVybiB7UHJvbWlzZTx7eDogbnVtYmVyLCB5OiBudW1iZXJ9Pn0gVGhlIHBvc2l0aW9uIG9mIHRoZSB3aW5kb3dcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd0dldFBvc2l0aW9uKCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOldpbmRvd0dldFBvc1wiKTtcbn1cblxuLyoqXG4gKiBIaWRlIHRoZSBXaW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dIaWRlKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV0gnKTtcbn1cblxuLyoqXG4gKiBTaG93IHRoZSBXaW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dTaG93KCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV1MnKTtcbn1cblxuLyoqXG4gKiBNYXhpbWlzZSB0aGUgV2luZG93XG4gKlxuICogQGV4cG9ydFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93TWF4aW1pc2UoKSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXTScpO1xufVxuXG4vKipcbiAqIFRvZ2dsZSB0aGUgTWF4aW1pc2Ugb2YgdGhlIFdpbmRvd1xuICpcbiAqIEBleHBvcnRcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1RvZ2dsZU1heGltaXNlKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV3QnKTtcbn1cblxuLyoqXG4gKiBVbm1heGltaXNlIHRoZSBXaW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dVbm1heGltaXNlKCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnV1UnKTtcbn1cblxuLyoqXG4gKiBSZXR1cm5zIHRoZSBzdGF0ZSBvZiB0aGUgd2luZG93LCBpLmUuIHdoZXRoZXIgdGhlIHdpbmRvdyBpcyBtYXhpbWlzZWQgb3Igbm90LlxuICpcbiAqIEBleHBvcnRcbiAqIEByZXR1cm4ge1Byb21pc2U8Ym9vbGVhbj59IFRoZSBzdGF0ZSBvZiB0aGUgd2luZG93XG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBXaW5kb3dJc01heGltaXNlZCgpIHtcbiAgICByZXR1cm4gQ2FsbChcIjp3YWlsczpXaW5kb3dJc01heGltaXNlZFwiKTtcbn1cblxuLyoqXG4gKiBNaW5pbWlzZSB0aGUgV2luZG93XG4gKlxuICogQGV4cG9ydFxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93TWluaW1pc2UoKSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXbScpO1xufVxuXG4vKipcbiAqIFVubWluaW1pc2UgdGhlIFdpbmRvd1xuICpcbiAqIEBleHBvcnRcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd1VubWluaW1pc2UoKSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdXdScpO1xufVxuXG4vKipcbiAqIFJldHVybnMgdGhlIHN0YXRlIG9mIHRoZSB3aW5kb3csIGkuZS4gd2hldGhlciB0aGUgd2luZG93IGlzIG1pbmltaXNlZCBvciBub3QuXG4gKlxuICogQGV4cG9ydFxuICogQHJldHVybiB7UHJvbWlzZTxib29sZWFuPn0gVGhlIHN0YXRlIG9mIHRoZSB3aW5kb3dcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd0lzTWluaW1pc2VkKCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOldpbmRvd0lzTWluaW1pc2VkXCIpO1xufVxuXG4vKipcbiAqIFJldHVybnMgdGhlIHN0YXRlIG9mIHRoZSB3aW5kb3csIGkuZS4gd2hldGhlciB0aGUgd2luZG93IGlzIG5vcm1hbCBvciBub3QuXG4gKlxuICogQGV4cG9ydFxuICogQHJldHVybiB7UHJvbWlzZTxib29sZWFuPn0gVGhlIHN0YXRlIG9mIHRoZSB3aW5kb3dcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFdpbmRvd0lzTm9ybWFsKCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOldpbmRvd0lzTm9ybWFsXCIpO1xufVxuXG4vKipcbiAqIFNldHMgdGhlIGJhY2tncm91bmQgY29sb3VyIG9mIHRoZSB3aW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge251bWJlcn0gUiBSZWRcbiAqIEBwYXJhbSB7bnVtYmVyfSBHIEdyZWVuXG4gKiBAcGFyYW0ge251bWJlcn0gQiBCbHVlXG4gKiBAcGFyYW0ge251bWJlcn0gQSBBbHBoYVxuICovXG5leHBvcnQgZnVuY3Rpb24gV2luZG93U2V0QmFja2dyb3VuZENvbG91cihSLCBHLCBCLCBBKSB7XG4gICAgbGV0IHJnYmEgPSBKU09OLnN0cmluZ2lmeSh7cjogUiB8fCAwLCBnOiBHIHx8IDAsIGI6IEIgfHwgMCwgYTogQSB8fCAyNTV9KTtcbiAgICB3aW5kb3cuV2FpbHNJbnZva2UoJ1dyOicgKyByZ2JhKTtcbn1cblxuIiwgIi8qXG4gX1x0ICAgX19cdCAgXyBfX1xufCB8XHQgLyAvX19fIF8oXykgL19fX19cbnwgfCAvfCAvIC8gX18gYC8gLyAvIF9fXy9cbnwgfC8gfC8gLyAvXy8gLyAvIChfXyAgKVxufF9fL3xfXy9cXF9fLF8vXy9fL19fX18vXG5UaGUgZWxlY3Ryb24gYWx0ZXJuYXRpdmUgZm9yIEdvXG4oYykgTGVhIEFudGhvbnkgMjAxOS1wcmVzZW50XG4qL1xuXG4vKiBqc2hpbnQgZXN2ZXJzaW9uOiA5ICovXG5cblxuaW1wb3J0IHtDYWxsfSBmcm9tIFwiLi9jYWxsc1wiO1xuXG5cbi8qKlxuICogR2V0cyB0aGUgYWxsIHNjcmVlbnMuIENhbGwgdGhpcyBhbmV3IGVhY2ggdGltZSB5b3Ugd2FudCB0byByZWZyZXNoIGRhdGEgZnJvbSB0aGUgdW5kZXJseWluZyB3aW5kb3dpbmcgc3lzdGVtLlxuICogQGV4cG9ydFxuICogQHR5cGVkZWYge2ltcG9ydCgnLi4vd3JhcHBlci9ydW50aW1lJykuU2NyZWVufSBTY3JlZW5cbiAqIEByZXR1cm4ge1Byb21pc2U8e1NjcmVlbltdfT59IFRoZSBzY3JlZW5zXG4gKi9cbmV4cG9ydCBmdW5jdGlvbiBTY3JlZW5HZXRBbGwoKSB7XG4gICAgcmV0dXJuIENhbGwoXCI6d2FpbHM6U2NyZWVuR2V0QWxsXCIpO1xufVxuIiwgIi8qKlxuICogQGRlc2NyaXB0aW9uOiBVc2UgdGhlIHN5c3RlbSBkZWZhdWx0IGJyb3dzZXIgdG8gb3BlbiB0aGUgdXJsXG4gKiBAcGFyYW0ge3N0cmluZ30gdXJsIFxuICogQHJldHVybiB7dm9pZH1cbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIEJyb3dzZXJPcGVuVVJMKHVybCkge1xuICB3aW5kb3cuV2FpbHNJbnZva2UoJ0JPOicgKyB1cmwpO1xufSIsICIvKlxuIF9cdCAgIF9fXHQgIF8gX19cbnwgfFx0IC8gL19fXyBfKF8pIC9fX19fXG58IHwgL3wgLyAvIF9fIGAvIC8gLyBfX18vXG58IHwvIHwvIC8gL18vIC8gLyAoX18gIClcbnxfXy98X18vXFxfXyxfL18vXy9fX19fL1xuVGhlIGVsZWN0cm9uIGFsdGVybmF0aXZlIGZvciBHb1xuKGMpIExlYSBBbnRob255IDIwMTktcHJlc2VudFxuKi9cblxuLyoganNoaW50IGVzdmVyc2lvbjogOSAqL1xuXG5pbXBvcnQge0NhbGx9IGZyb20gXCIuL2NhbGxzXCI7XG5cbi8qKlxuICogU2V0IHRoZSBTaXplIG9mIHRoZSB3aW5kb3dcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcGFyYW0ge3N0cmluZ30gdGV4dFxuICovXG5leHBvcnQgZnVuY3Rpb24gQ2xpcGJvYXJkU2V0VGV4dCh0ZXh0KSB7XG4gICAgcmV0dXJuIENhbGwoXCI6d2FpbHM6Q2xpcGJvYXJkU2V0VGV4dFwiLCBbdGV4dF0pO1xufVxuXG4vKipcbiAqIEdldCB0aGUgdGV4dCBjb250ZW50IG9mIHRoZSBjbGlwYm9hcmRcbiAqXG4gKiBAZXhwb3J0XG4gKiBAcmV0dXJuIHtQcm9taXNlPHtzdHJpbmd9Pn0gVGV4dCBjb250ZW50IG9mIHRoZSBjbGlwYm9hcmRcblxuICovXG5leHBvcnQgZnVuY3Rpb24gQ2xpcGJvYXJkR2V0VGV4dCgpIHtcbiAgICByZXR1cm4gQ2FsbChcIjp3YWlsczpDbGlwYm9hcmRHZXRUZXh0XCIpO1xufSIsICIvKlxuIF9cdCAgIF9fXHQgIF8gX19cbnwgfFx0IC8gL19fXyBfKF8pIC9fX19fXG58IHwgL3wgLyAvIF9fIGAvIC8gLyBfX18vXG58IHwvIHwvIC8gL18vIC8gLyAoX18gIClcbnxfXy98X18vXFxfXyxfL18vXy9fX19fL1xuVGhlIGVsZWN0cm9uIGFsdGVybmF0aXZlIGZvciBHb1xuKGMpIExlYSBBbnRob255IDIwMTktcHJlc2VudFxuKi9cblxuLyoganNoaW50IGVzdmVyc2lvbjogOSAqL1xuXG5pbXBvcnQge0V2ZW50c09uLCBFdmVudHNPZmZ9IGZyb20gXCIuL2V2ZW50c1wiO1xuXG5jb25zdCBmbGFncyA9IHtcbiAgICByZWdpc3RlcmVkOiBmYWxzZSxcbiAgICBkZWZhdWx0VXNlRHJvcFRhcmdldDogdHJ1ZSxcbiAgICB1c2VEcm9wVGFyZ2V0OiB0cnVlLFxuICAgIG5leHREZWFjdGl2YXRlOiBudWxsLFxuICAgIG5leHREZWFjdGl2YXRlVGltZW91dDogbnVsbCxcbn07XG5cbmNvbnN0IERST1BfVEFSR0VUX0FDVElWRSA9IFwid2FpbHMtZHJvcC10YXJnZXQtYWN0aXZlXCI7XG5cbi8qKlxuICogY2hlY2tTdHlsZURyb3BUYXJnZXQgY2hlY2tzIGlmIHRoZSBzdHlsZSBoYXMgdGhlIGRyb3AgdGFyZ2V0IGF0dHJpYnV0ZVxuICogXG4gKiBAcGFyYW0ge0NTU1N0eWxlRGVjbGFyYXRpb259IHN0eWxlIFxuICogQHJldHVybnMgXG4gKi9cbmZ1bmN0aW9uIGNoZWNrU3R5bGVEcm9wVGFyZ2V0KHN0eWxlKSB7XG4gICAgY29uc3QgY3NzRHJvcFZhbHVlID0gc3R5bGUuZ2V0UHJvcGVydHlWYWx1ZSh3aW5kb3cud2FpbHMuZmxhZ3MuY3NzRHJvcFByb3BlcnR5KS50cmltKCk7XG4gICAgaWYgKGNzc0Ryb3BWYWx1ZSkge1xuICAgICAgICBpZiAoY3NzRHJvcFZhbHVlID09PSB3aW5kb3cud2FpbHMuZmxhZ3MuY3NzRHJvcFZhbHVlKSB7XG4gICAgICAgICAgICByZXR1cm4gdHJ1ZTtcbiAgICAgICAgfVxuICAgICAgICAvLyBpZiB0aGUgZWxlbWVudCBoYXMgdGhlIGRyb3AgdGFyZ2V0IGF0dHJpYnV0ZSwgYnV0IFxuICAgICAgICAvLyB0aGUgdmFsdWUgaXMgbm90IGNvcnJlY3QsIHRlcm1pbmF0ZSBmaW5kaW5nIHByb2Nlc3MuXG4gICAgICAgIC8vIFRoaXMgY2FuIGJlIHVzZWZ1bCB0byBibG9jayBzb21lIGNoaWxkIGVsZW1lbnRzIGZyb20gYmVpbmcgZHJvcCB0YXJnZXRzLlxuICAgICAgICByZXR1cm4gZmFsc2U7XG4gICAgfVxuICAgIHJldHVybiBmYWxzZTtcbn1cblxuLyoqXG4gKiBvbkRyYWdPdmVyIGlzIGNhbGxlZCB3aGVuIHRoZSBkcmFnb3ZlciBldmVudCBpcyBlbWl0dGVkLlxuICogQHBhcmFtIHtEcmFnRXZlbnR9IGVcbiAqIEByZXR1cm5zXG4gKi9cbmZ1bmN0aW9uIG9uRHJhZ092ZXIoZSkge1xuICAgIC8vIENoZWNrIGlmIHRoaXMgaXMgYW4gZXh0ZXJuYWwgZmlsZSBkcm9wIG9yIGludGVybmFsIEhUTUwgZHJhZ1xuICAgIC8vIEV4dGVybmFsIGZpbGUgZHJvcHMgd2lsbCBoYXZlIFwiRmlsZXNcIiBpbiB0aGUgdHlwZXMgYXJyYXlcbiAgICAvLyBJbnRlcm5hbCBIVE1MIGRyYWdzIHR5cGljYWxseSBoYXZlIFwidGV4dC9wbGFpblwiLCBcInRleHQvaHRtbFwiIG9yIGN1c3RvbSB0eXBlc1xuICAgIGNvbnN0IGlzRmlsZURyb3AgPSBlLmRhdGFUcmFuc2Zlci50eXBlcy5pbmNsdWRlcyhcIkZpbGVzXCIpO1xuXG4gICAgLy8gT25seSBoYW5kbGUgZXh0ZXJuYWwgZmlsZSBkcm9wcywgbGV0IGludGVybmFsIEhUTUw1IGRyYWctYW5kLWRyb3Agd29yayBub3JtYWxseVxuICAgIGlmICghaXNGaWxlRHJvcCkge1xuICAgICAgICByZXR1cm47XG4gICAgfVxuXG4gICAgLy8gQUxXQVlTIHByZXZlbnQgZGVmYXVsdCBmb3IgZmlsZSBkcm9wcyB0byBzdG9wIGJyb3dzZXIgbmF2aWdhdGlvblxuICAgIGUucHJldmVudERlZmF1bHQoKTtcbiAgICBlLmRhdGFUcmFuc2Zlci5kcm9wRWZmZWN0ID0gJ2NvcHknO1xuXG4gICAgaWYgKCF3aW5kb3cud2FpbHMuZmxhZ3MuZW5hYmxlV2FpbHNEcmFnQW5kRHJvcCkge1xuICAgICAgICByZXR1cm47XG4gICAgfVxuXG4gICAgaWYgKCFmbGFncy51c2VEcm9wVGFyZ2V0KSB7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG5cbiAgICBjb25zdCBlbGVtZW50ID0gZS50YXJnZXQ7XG5cbiAgICAvLyBUcmlnZ2VyIGRlYm91bmNlIGZ1bmN0aW9uIHRvIGRlYWN0aXZhdGUgZHJvcCB0YXJnZXRzXG4gICAgaWYoZmxhZ3MubmV4dERlYWN0aXZhdGUpIGZsYWdzLm5leHREZWFjdGl2YXRlKCk7XG5cbiAgICAvLyBpZiB0aGUgZWxlbWVudCBpcyBudWxsIG9yIGVsZW1lbnQgaXMgbm90IGNoaWxkIG9mIGRyb3AgdGFyZ2V0IGVsZW1lbnRcbiAgICBpZiAoIWVsZW1lbnQgfHwgIWNoZWNrU3R5bGVEcm9wVGFyZ2V0KGdldENvbXB1dGVkU3R5bGUoZWxlbWVudCkpKSB7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG5cbiAgICBsZXQgY3VycmVudEVsZW1lbnQgPSBlbGVtZW50O1xuICAgIHdoaWxlIChjdXJyZW50RWxlbWVudCkge1xuICAgICAgICAvLyBjaGVjayBpZiBjdXJyZW50RWxlbWVudCBpcyBkcm9wIHRhcmdldCBlbGVtZW50XG4gICAgICAgIGlmIChjaGVja1N0eWxlRHJvcFRhcmdldChnZXRDb21wdXRlZFN0eWxlKGN1cnJlbnRFbGVtZW50KSkpIHtcbiAgICAgICAgICAgIGN1cnJlbnRFbGVtZW50LmNsYXNzTGlzdC5hZGQoRFJPUF9UQVJHRVRfQUNUSVZFKTtcbiAgICAgICAgfVxuICAgICAgICBjdXJyZW50RWxlbWVudCA9IGN1cnJlbnRFbGVtZW50LnBhcmVudEVsZW1lbnQ7XG4gICAgfVxufVxuXG4vKipcbiAqIG9uRHJhZ0xlYXZlIGlzIGNhbGxlZCB3aGVuIHRoZSBkcmFnbGVhdmUgZXZlbnQgaXMgZW1pdHRlZC5cbiAqIEBwYXJhbSB7RHJhZ0V2ZW50fSBlXG4gKiBAcmV0dXJuc1xuICovXG5mdW5jdGlvbiBvbkRyYWdMZWF2ZShlKSB7XG4gICAgLy8gQ2hlY2sgaWYgdGhpcyBpcyBhbiBleHRlcm5hbCBmaWxlIGRyb3Agb3IgaW50ZXJuYWwgSFRNTCBkcmFnXG4gICAgY29uc3QgaXNGaWxlRHJvcCA9IGUuZGF0YVRyYW5zZmVyLnR5cGVzLmluY2x1ZGVzKFwiRmlsZXNcIik7XG5cbiAgICAvLyBPbmx5IGhhbmRsZSBleHRlcm5hbCBmaWxlIGRyb3BzLCBsZXQgaW50ZXJuYWwgSFRNTDUgZHJhZy1hbmQtZHJvcCB3b3JrIG5vcm1hbGx5XG4gICAgaWYgKCFpc0ZpbGVEcm9wKSB7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG5cbiAgICAvLyBBTFdBWVMgcHJldmVudCBkZWZhdWx0IGZvciBmaWxlIGRyb3BzIHRvIHN0b3AgYnJvd3NlciBuYXZpZ2F0aW9uXG4gICAgZS5wcmV2ZW50RGVmYXVsdCgpO1xuXG4gICAgaWYgKCF3aW5kb3cud2FpbHMuZmxhZ3MuZW5hYmxlV2FpbHNEcmFnQW5kRHJvcCkge1xuICAgICAgICByZXR1cm47XG4gICAgfVxuXG4gICAgaWYgKCFmbGFncy51c2VEcm9wVGFyZ2V0KSB7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG5cbiAgICAvLyBGaW5kIHRoZSBjbG9zZSBkcm9wIHRhcmdldCBlbGVtZW50XG4gICAgaWYgKCFlLnRhcmdldCB8fCAhY2hlY2tTdHlsZURyb3BUYXJnZXQoZ2V0Q29tcHV0ZWRTdHlsZShlLnRhcmdldCkpKSB7XG4gICAgICAgIHJldHVybiBudWxsO1xuICAgIH1cblxuICAgIC8vIFRyaWdnZXIgZGVib3VuY2UgZnVuY3Rpb24gdG8gZGVhY3RpdmF0ZSBkcm9wIHRhcmdldHNcbiAgICBpZihmbGFncy5uZXh0RGVhY3RpdmF0ZSkgZmxhZ3MubmV4dERlYWN0aXZhdGUoKTtcbiAgICBcbiAgICAvLyBVc2UgZGVib3VuY2UgdGVjaG5pcXVlIHRvIHRhY2xlIGRyYWdsZWF2ZSBldmVudHMgb24gb3ZlcmxhcHBpbmcgZWxlbWVudHMgYW5kIGRyb3AgdGFyZ2V0IGVsZW1lbnRzXG4gICAgZmxhZ3MubmV4dERlYWN0aXZhdGUgPSAoKSA9PiB7XG4gICAgICAgIC8vIERlYWN0aXZhdGUgYWxsIGRyb3AgdGFyZ2V0cywgbmV3IGRyb3AgdGFyZ2V0IHdpbGwgYmUgYWN0aXZhdGVkIG9uIG5leHQgZHJhZ292ZXIgZXZlbnRcbiAgICAgICAgQXJyYXkuZnJvbShkb2N1bWVudC5nZXRFbGVtZW50c0J5Q2xhc3NOYW1lKERST1BfVEFSR0VUX0FDVElWRSkpLmZvckVhY2goZWwgPT4gZWwuY2xhc3NMaXN0LnJlbW92ZShEUk9QX1RBUkdFVF9BQ1RJVkUpKTtcbiAgICAgICAgLy8gUmVzZXQgbmV4dERlYWN0aXZhdGVcbiAgICAgICAgZmxhZ3MubmV4dERlYWN0aXZhdGUgPSBudWxsO1xuICAgICAgICAvLyBDbGVhciB0aW1lb3V0XG4gICAgICAgIGlmIChmbGFncy5uZXh0RGVhY3RpdmF0ZVRpbWVvdXQpIHtcbiAgICAgICAgICAgIGNsZWFyVGltZW91dChmbGFncy5uZXh0RGVhY3RpdmF0ZVRpbWVvdXQpO1xuICAgICAgICAgICAgZmxhZ3MubmV4dERlYWN0aXZhdGVUaW1lb3V0ID0gbnVsbDtcbiAgICAgICAgfVxuICAgIH1cblxuICAgIC8vIFNldCB0aW1lb3V0IHRvIGRlYWN0aXZhdGUgZHJvcCB0YXJnZXRzIGlmIG5vdCB0cmlnZ2VyZWQgYnkgbmV4dCBkcmFnIGV2ZW50XG4gICAgZmxhZ3MubmV4dERlYWN0aXZhdGVUaW1lb3V0ID0gc2V0VGltZW91dCgoKSA9PiB7XG4gICAgICAgIGlmKGZsYWdzLm5leHREZWFjdGl2YXRlKSBmbGFncy5uZXh0RGVhY3RpdmF0ZSgpO1xuICAgIH0sIDUwKTtcbn1cblxuLyoqXG4gKiBvbkRyb3AgaXMgY2FsbGVkIHdoZW4gdGhlIGRyb3AgZXZlbnQgaXMgZW1pdHRlZC5cbiAqIEBwYXJhbSB7RHJhZ0V2ZW50fSBlXG4gKiBAcmV0dXJuc1xuICovXG5mdW5jdGlvbiBvbkRyb3AoZSkge1xuICAgIC8vIENoZWNrIGlmIHRoaXMgaXMgYW4gZXh0ZXJuYWwgZmlsZSBkcm9wIG9yIGludGVybmFsIEhUTUwgZHJhZ1xuICAgIGNvbnN0IGlzRmlsZURyb3AgPSBlLmRhdGFUcmFuc2Zlci50eXBlcy5pbmNsdWRlcyhcIkZpbGVzXCIpO1xuXG4gICAgLy8gT25seSBoYW5kbGUgZXh0ZXJuYWwgZmlsZSBkcm9wcywgbGV0IGludGVybmFsIEhUTUw1IGRyYWctYW5kLWRyb3Agd29yayBub3JtYWxseVxuICAgIGlmICghaXNGaWxlRHJvcCkge1xuICAgICAgICByZXR1cm47XG4gICAgfVxuXG4gICAgLy8gQUxXQVlTIHByZXZlbnQgZGVmYXVsdCBmb3IgZmlsZSBkcm9wcyB0byBzdG9wIGJyb3dzZXIgbmF2aWdhdGlvblxuICAgIGUucHJldmVudERlZmF1bHQoKTtcblxuICAgIGlmICghd2luZG93LndhaWxzLmZsYWdzLmVuYWJsZVdhaWxzRHJhZ0FuZERyb3ApIHtcbiAgICAgICAgcmV0dXJuO1xuICAgIH1cblxuICAgIGlmIChDYW5SZXNvbHZlRmlsZVBhdGhzKCkpIHtcbiAgICAgICAgLy8gcHJvY2VzcyBmaWxlc1xuICAgICAgICBsZXQgZmlsZXMgPSBbXTtcbiAgICAgICAgaWYgKGUuZGF0YVRyYW5zZmVyLml0ZW1zKSB7XG4gICAgICAgICAgICBmaWxlcyA9IFsuLi5lLmRhdGFUcmFuc2Zlci5pdGVtc10ubWFwKChpdGVtLCBpKSA9PiB7XG4gICAgICAgICAgICAgICAgaWYgKGl0ZW0ua2luZCA9PT0gJ2ZpbGUnKSB7XG4gICAgICAgICAgICAgICAgICAgIHJldHVybiBpdGVtLmdldEFzRmlsZSgpO1xuICAgICAgICAgICAgICAgIH1cbiAgICAgICAgICAgIH0pO1xuICAgICAgICB9IGVsc2Uge1xuICAgICAgICAgICAgZmlsZXMgPSBbLi4uZS5kYXRhVHJhbnNmZXIuZmlsZXNdO1xuICAgICAgICB9XG4gICAgICAgIHdpbmRvdy5ydW50aW1lLlJlc29sdmVGaWxlUGF0aHMoZS54LCBlLnksIGZpbGVzKTtcbiAgICB9XG5cbiAgICBpZiAoIWZsYWdzLnVzZURyb3BUYXJnZXQpIHtcbiAgICAgICAgcmV0dXJuO1xuICAgIH1cblxuICAgIC8vIFRyaWdnZXIgZGVib3VuY2UgZnVuY3Rpb24gdG8gZGVhY3RpdmF0ZSBkcm9wIHRhcmdldHNcbiAgICBpZihmbGFncy5uZXh0RGVhY3RpdmF0ZSkgZmxhZ3MubmV4dERlYWN0aXZhdGUoKTtcblxuICAgIC8vIERlYWN0aXZhdGUgYWxsIGRyb3AgdGFyZ2V0c1xuICAgIEFycmF5LmZyb20oZG9jdW1lbnQuZ2V0RWxlbWVudHNCeUNsYXNzTmFtZShEUk9QX1RBUkdFVF9BQ1RJVkUpKS5mb3JFYWNoKGVsID0+IGVsLmNsYXNzTGlzdC5yZW1vdmUoRFJPUF9UQVJHRVRfQUNUSVZFKSk7XG59XG5cbi8qKlxuICogcG9zdE1lc3NhZ2VXaXRoQWRkaXRpb25hbE9iamVjdHMgY2hlY2tzIHRoZSBicm93c2VyJ3MgY2FwYWJpbGl0eSBvZiBzZW5kaW5nIHBvc3RNZXNzYWdlV2l0aEFkZGl0aW9uYWxPYmplY3RzXG4gKlxuICogQHJldHVybnMge2Jvb2xlYW59XG4gKiBAY29uc3RydWN0b3JcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIENhblJlc29sdmVGaWxlUGF0aHMoKSB7XG4gICAgcmV0dXJuIHdpbmRvdy5jaHJvbWU/LndlYnZpZXc/LnBvc3RNZXNzYWdlV2l0aEFkZGl0aW9uYWxPYmplY3RzICE9IG51bGw7XG59XG5cbi8qKlxuICogUmVzb2x2ZUZpbGVQYXRocyBzZW5kcyBkcm9wIGV2ZW50cyB0byB0aGUgR08gc2lkZSB0byByZXNvbHZlIGZpbGUgcGF0aHMgb24gd2luZG93cy5cbiAqXG4gKiBAcGFyYW0ge251bWJlcn0geFxuICogQHBhcmFtIHtudW1iZXJ9IHlcbiAqIEBwYXJhbSB7YW55W119IGZpbGVzXG4gKiBAY29uc3RydWN0b3JcbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIFJlc29sdmVGaWxlUGF0aHMoeCwgeSwgZmlsZXMpIHtcbiAgICAvLyBPbmx5IGZvciB3aW5kb3dzIHdlYnZpZXcyID49IDEuMC4xNzc0LjMwXG4gICAgLy8gaHR0cHM6Ly9sZWFybi5taWNyb3NvZnQuY29tL2VuLXVzL21pY3Jvc29mdC1lZGdlL3dlYnZpZXcyL3JlZmVyZW5jZS93aW4zMi9pY29yZXdlYnZpZXcyd2VibWVzc2FnZXJlY2VpdmVkZXZlbnRhcmdzMj92aWV3PXdlYnZpZXcyLTEuMC4xODIzLjMyI2FwcGxpZXMtdG9cbiAgICBpZiAod2luZG93LmNocm9tZT8ud2Vidmlldz8ucG9zdE1lc3NhZ2VXaXRoQWRkaXRpb25hbE9iamVjdHMpIHtcbiAgICAgICAgY2hyb21lLndlYnZpZXcucG9zdE1lc3NhZ2VXaXRoQWRkaXRpb25hbE9iamVjdHMoYGZpbGU6ZHJvcDoke3h9OiR7eX1gLCBmaWxlcyk7XG4gICAgfVxufVxuXG4vKipcbiAqIENhbGxiYWNrIGZvciBPbkZpbGVEcm9wIHJldHVybnMgYSBzbGljZSBvZiBmaWxlIHBhdGggc3RyaW5ncyB3aGVuIGEgZHJvcCBpcyBmaW5pc2hlZC5cbiAqXG4gKiBAZXhwb3J0XG4gKiBAY2FsbGJhY2sgT25GaWxlRHJvcENhbGxiYWNrXG4gKiBAcGFyYW0ge251bWJlcn0geCAtIHggY29vcmRpbmF0ZSBvZiB0aGUgZHJvcFxuICogQHBhcmFtIHtudW1iZXJ9IHkgLSB5IGNvb3JkaW5hdGUgb2YgdGhlIGRyb3BcbiAqIEBwYXJhbSB7c3RyaW5nW119IHBhdGhzIC0gQSBsaXN0IG9mIGZpbGUgcGF0aHMuXG4gKi9cblxuLyoqXG4gKiBPbkZpbGVEcm9wIGxpc3RlbnMgdG8gZHJhZyBhbmQgZHJvcCBldmVudHMgYW5kIGNhbGxzIHRoZSBjYWxsYmFjayB3aXRoIHRoZSBjb29yZGluYXRlcyBvZiB0aGUgZHJvcCBhbmQgYW4gYXJyYXkgb2YgcGF0aCBzdHJpbmdzLlxuICpcbiAqIEBleHBvcnRcbiAqIEBwYXJhbSB7T25GaWxlRHJvcENhbGxiYWNrfSBjYWxsYmFjayAtIENhbGxiYWNrIGZvciBPbkZpbGVEcm9wIHJldHVybnMgYSBzbGljZSBvZiBmaWxlIHBhdGggc3RyaW5ncyB3aGVuIGEgZHJvcCBpcyBmaW5pc2hlZC5cbiAqIEBwYXJhbSB7Ym9vbGVhbn0gW3VzZURyb3BUYXJnZXQ9dHJ1ZV0gLSBPbmx5IGNhbGwgdGhlIGNhbGxiYWNrIHdoZW4gdGhlIGRyb3AgZmluaXNoZWQgb24gYW4gZWxlbWVudCB0aGF0IGhhcyB0aGUgZHJvcCB0YXJnZXQgc3R5bGUuICgtLXdhaWxzLWRyb3AtdGFyZ2V0KVxuICovXG5leHBvcnQgZnVuY3Rpb24gT25GaWxlRHJvcChjYWxsYmFjaywgdXNlRHJvcFRhcmdldCkge1xuICAgIGlmICh0eXBlb2YgY2FsbGJhY2sgIT09IFwiZnVuY3Rpb25cIikge1xuICAgICAgICBjb25zb2xlLmVycm9yKFwiRHJhZ0FuZERyb3BDYWxsYmFjayBpcyBub3QgYSBmdW5jdGlvblwiKTtcbiAgICAgICAgcmV0dXJuO1xuICAgIH1cblxuICAgIGlmIChmbGFncy5yZWdpc3RlcmVkKSB7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG4gICAgZmxhZ3MucmVnaXN0ZXJlZCA9IHRydWU7XG5cbiAgICBjb25zdCB1RFRQVCA9IHR5cGVvZiB1c2VEcm9wVGFyZ2V0O1xuICAgIGZsYWdzLnVzZURyb3BUYXJnZXQgPSB1RFRQVCA9PT0gXCJ1bmRlZmluZWRcIiB8fCB1RFRQVCAhPT0gXCJib29sZWFuXCIgPyBmbGFncy5kZWZhdWx0VXNlRHJvcFRhcmdldCA6IHVzZURyb3BUYXJnZXQ7XG4gICAgd2luZG93LmFkZEV2ZW50TGlzdGVuZXIoJ2RyYWdvdmVyJywgb25EcmFnT3Zlcik7XG4gICAgd2luZG93LmFkZEV2ZW50TGlzdGVuZXIoJ2RyYWdsZWF2ZScsIG9uRHJhZ0xlYXZlKTtcbiAgICB3aW5kb3cuYWRkRXZlbnRMaXN0ZW5lcignZHJvcCcsIG9uRHJvcCk7XG5cbiAgICBsZXQgY2IgPSBjYWxsYmFjaztcbiAgICBpZiAoZmxhZ3MudXNlRHJvcFRhcmdldCkge1xuICAgICAgICBjYiA9IGZ1bmN0aW9uICh4LCB5LCBwYXRocykge1xuICAgICAgICAgICAgY29uc3QgZWxlbWVudCA9IGRvY3VtZW50LmVsZW1lbnRGcm9tUG9pbnQoeCwgeSlcbiAgICAgICAgICAgIC8vIGlmIHRoZSBlbGVtZW50IGlzIG51bGwgb3IgZWxlbWVudCBpcyBub3QgY2hpbGQgb2YgZHJvcCB0YXJnZXQgZWxlbWVudCwgcmV0dXJuIG51bGxcbiAgICAgICAgICAgIGlmICghZWxlbWVudCB8fCAhY2hlY2tTdHlsZURyb3BUYXJnZXQoZ2V0Q29tcHV0ZWRTdHlsZShlbGVtZW50KSkpIHtcbiAgICAgICAgICAgICAgICByZXR1cm4gbnVsbDtcbiAgICAgICAgICAgIH1cbiAgICAgICAgICAgIGNhbGxiYWNrKHgsIHksIHBhdGhzKTtcbiAgICAgICAgfVxuICAgIH1cblxuICAgIEV2ZW50c09uKFwid2FpbHM6ZmlsZS1kcm9wXCIsIGNiKTtcbn1cblxuLyoqXG4gKiBPbkZpbGVEcm9wT2ZmIHJlbW92ZXMgdGhlIGRyYWcgYW5kIGRyb3AgbGlzdGVuZXJzIGFuZCBoYW5kbGVycy5cbiAqL1xuZXhwb3J0IGZ1bmN0aW9uIE9uRmlsZURyb3BPZmYoKSB7XG4gICAgd2luZG93LnJlbW92ZUV2ZW50TGlzdGVuZXIoJ2RyYWdvdmVyJywgb25EcmFnT3Zlcik7XG4gICAgd2luZG93LnJlbW92ZUV2ZW50TGlzdGVuZXIoJ2RyYWdsZWF2ZScsIG9uRHJhZ0xlYXZlKTtcbiAgICB3aW5kb3cucmVtb3ZlRXZlbnRMaXN0ZW5lcignZHJvcCcsIG9uRHJvcCk7XG4gICAgRXZlbnRzT2ZmKFwid2FpbHM6ZmlsZS1kcm9wXCIpO1xuICAgIGZsYWdzLnJlZ2lzdGVyZWQgPSBmYWxzZTtcbn1cbiIsICIvKlxuLS1kZWZhdWx0LWNvbnRleHRtZW51OiBhdXRvOyAoZGVmYXVsdCkgd2lsbCBzaG93IHRoZSBkZWZhdWx0IGNvbnRleHQgbWVudSBpZiBjb250ZW50RWRpdGFibGUgaXMgdHJ1ZSBPUiB0ZXh0IGhhcyBiZWVuIHNlbGVjdGVkIE9SIGVsZW1lbnQgaXMgaW5wdXQgb3IgdGV4dGFyZWFcbi0tZGVmYXVsdC1jb250ZXh0bWVudTogc2hvdzsgd2lsbCBhbHdheXMgc2hvdyB0aGUgZGVmYXVsdCBjb250ZXh0IG1lbnVcbi0tZGVmYXVsdC1jb250ZXh0bWVudTogaGlkZTsgd2lsbCBhbHdheXMgaGlkZSB0aGUgZGVmYXVsdCBjb250ZXh0IG1lbnVcblxuVGhpcyBydWxlIGlzIGluaGVyaXRlZCBsaWtlIG5vcm1hbCBDU1MgcnVsZXMsIHNvIG5lc3Rpbmcgd29ya3MgYXMgZXhwZWN0ZWRcbiovXG5leHBvcnQgZnVuY3Rpb24gcHJvY2Vzc0RlZmF1bHRDb250ZXh0TWVudShldmVudCkge1xuICAgIC8vIFByb2Nlc3MgZGVmYXVsdCBjb250ZXh0IG1lbnVcbiAgICBjb25zdCBlbGVtZW50ID0gZXZlbnQudGFyZ2V0O1xuICAgIGNvbnN0IGNvbXB1dGVkU3R5bGUgPSB3aW5kb3cuZ2V0Q29tcHV0ZWRTdHlsZShlbGVtZW50KTtcbiAgICBjb25zdCBkZWZhdWx0Q29udGV4dE1lbnVBY3Rpb24gPSBjb21wdXRlZFN0eWxlLmdldFByb3BlcnR5VmFsdWUoXCItLWRlZmF1bHQtY29udGV4dG1lbnVcIikudHJpbSgpO1xuICAgIHN3aXRjaCAoZGVmYXVsdENvbnRleHRNZW51QWN0aW9uKSB7XG4gICAgICAgIGNhc2UgXCJzaG93XCI6XG4gICAgICAgICAgICByZXR1cm47XG4gICAgICAgIGNhc2UgXCJoaWRlXCI6XG4gICAgICAgICAgICBldmVudC5wcmV2ZW50RGVmYXVsdCgpO1xuICAgICAgICAgICAgcmV0dXJuO1xuICAgICAgICBkZWZhdWx0OlxuICAgICAgICAgICAgLy8gQ2hlY2sgaWYgY29udGVudEVkaXRhYmxlIGlzIHRydWVcbiAgICAgICAgICAgIGlmIChlbGVtZW50LmlzQ29udGVudEVkaXRhYmxlKSB7XG4gICAgICAgICAgICAgICAgcmV0dXJuO1xuICAgICAgICAgICAgfVxuXG4gICAgICAgICAgICAvLyBDaGVjayBpZiB0ZXh0IGhhcyBiZWVuIHNlbGVjdGVkIGFuZCBhY3Rpb24gaXMgb24gdGhlIHNlbGVjdGVkIGVsZW1lbnRzXG4gICAgICAgICAgICBjb25zdCBzZWxlY3Rpb24gPSB3aW5kb3cuZ2V0U2VsZWN0aW9uKCk7XG4gICAgICAgICAgICBjb25zdCBoYXNTZWxlY3Rpb24gPSAoc2VsZWN0aW9uLnRvU3RyaW5nKCkubGVuZ3RoID4gMClcbiAgICAgICAgICAgIGlmIChoYXNTZWxlY3Rpb24pIHtcbiAgICAgICAgICAgICAgICBmb3IgKGxldCBpID0gMDsgaSA8IHNlbGVjdGlvbi5yYW5nZUNvdW50OyBpKyspIHtcbiAgICAgICAgICAgICAgICAgICAgY29uc3QgcmFuZ2UgPSBzZWxlY3Rpb24uZ2V0UmFuZ2VBdChpKTtcbiAgICAgICAgICAgICAgICAgICAgY29uc3QgcmVjdHMgPSByYW5nZS5nZXRDbGllbnRSZWN0cygpO1xuICAgICAgICAgICAgICAgICAgICBmb3IgKGxldCBqID0gMDsgaiA8IHJlY3RzLmxlbmd0aDsgaisrKSB7XG4gICAgICAgICAgICAgICAgICAgICAgICBjb25zdCByZWN0ID0gcmVjdHNbal07XG4gICAgICAgICAgICAgICAgICAgICAgICBpZiAoZG9jdW1lbnQuZWxlbWVudEZyb21Qb2ludChyZWN0LmxlZnQsIHJlY3QudG9wKSA9PT0gZWxlbWVudCkge1xuICAgICAgICAgICAgICAgICAgICAgICAgICAgIHJldHVybjtcbiAgICAgICAgICAgICAgICAgICAgICAgIH1cbiAgICAgICAgICAgICAgICAgICAgfVxuICAgICAgICAgICAgICAgIH1cbiAgICAgICAgICAgIH1cbiAgICAgICAgICAgIC8vIENoZWNrIGlmIHRhZ25hbWUgaXMgaW5wdXQgb3IgdGV4dGFyZWFcbiAgICAgICAgICAgIGlmIChlbGVtZW50LnRhZ05hbWUgPT09IFwiSU5QVVRcIiB8fCBlbGVtZW50LnRhZ05hbWUgPT09IFwiVEVYVEFSRUFcIikge1xuICAgICAgICAgICAgICAgIGlmIChoYXNTZWxlY3Rpb24gfHwgKCFlbGVtZW50LnJlYWRPbmx5ICYmICFlbGVtZW50LmRpc2FibGVkKSkge1xuICAgICAgICAgICAgICAgICAgICByZXR1cm47XG4gICAgICAgICAgICAgICAgfVxuICAgICAgICAgICAgfVxuXG4gICAgICAgICAgICAvLyBoaWRlIGRlZmF1bHQgY29udGV4dCBtZW51XG4gICAgICAgICAgICBldmVudC5wcmV2ZW50RGVmYXVsdCgpO1xuICAgIH1cbn1cbiIsICIvLyByZXNpemVDdXJzb3JGb3JQb2ludGVyIGlzIHRoZSBmcmFtZWxlc3MgZWRnZSBjdXJzb3IsIG9yIFwiXCIgd2hlbiB0aGUgcG9pbnRlclxuLy8gaXMgbm90IG9uIGFuIGVkZ2UuIFwiXCIgaXMgYWxzbyByZXR1cm5lZCB3aGVuIHRoZSByZXBvcnRlZCB3aW5kb3cgc2l6ZSBjYW5ub3Rcbi8vIGJlIHRydXN0ZWQuIEEgbWF4aW1pc2VkIG9yIGZ1bGxzY3JlZW4gV2ViVmlldzIgd2luZG93IG9mdGVuIGxlYXZlc1xuLy8gb3V0ZXJXaWR0aC9vdXRlckhlaWdodCBhdCB0aGUgcmVzdG9yZWQgc2l6ZSwgb3IgYXQgMCwgd2hpbGUgdGhlIHBhZ2UgaXNcbi8vIGFscmVhZHkgdGhlIGZ1bGwgbW9uaXRvci4gVGhlIGVkZ2UgdGVzdCB0aGVuIG1hdGNoZXMgdGhlIHdob2xlIHdpbmRvdyBhbmRcbi8vIHNldHMgYSBzaXppbmcgY3Vyc29yIGV2ZW4gdGhvdWdoIG91dGVyLXdpbmRvdyByZXNpemluZyBpcyBpbXBvc3NpYmxlIHRoZXJlLlxuXG5jb25zdCBSRVNJWkVfQ1VSU09SUyA9IHtcbiAgICBzZTogXCJzZS1yZXNpemVcIixcbiAgICBzdzogXCJzdy1yZXNpemVcIixcbiAgICBudzogXCJudy1yZXNpemVcIixcbiAgICBuZTogXCJuZS1yZXNpemVcIixcbiAgICB3OiBcInctcmVzaXplXCIsXG4gICAgbjogXCJuLXJlc2l6ZVwiLFxuICAgIHM6IFwicy1yZXNpemVcIixcbiAgICBlOiBcImUtcmVzaXplXCIsXG59O1xuXG4vLyBHYXBzIHdpdGhpbiB0aGlzIG1hbnkgQ1NTIHBpeGVscyBhcmUgcm91bmRpbmcuIEEgbWF4aW1pc2VkIHdpbmRvdyB0aGF0XG4vLyBzdGlsbCByZXBvcnRzIHRoZSByZXN0b3JlZCBvdXRlciBzaXplIGlzIHNob3J0IGJ5IGEgbGFyZ2UgcGFydCBvZiB0aGUgbW9uaXRvci5cbmNvbnN0IFJFU0laRV9NRVRSSUNfU0xBQ0sgPSAzMjtcblxuZXhwb3J0IGZ1bmN0aW9uIGlzUmVzaXplQ3Vyc29yKGN1cnNvcikge1xuICAgIHJldHVybiB0eXBlb2YgY3Vyc29yID09PSBcInN0cmluZ1wiICYmIC9yZXNpemUkLy50ZXN0KGN1cnNvcik7XG59XG5cbmV4cG9ydCBmdW5jdGlvbiByZXNpemVDdXJzb3JGb3JQb2ludGVyKG91dGVyV2lkdGgsIG91dGVySGVpZ2h0LCBpbm5lcldpZHRoLCBpbm5lckhlaWdodCwgeCwgeSwgYm9yZGVyKSB7XG4gICAgaWYgKCEob3V0ZXJXaWR0aCA+IDApIHx8ICEob3V0ZXJIZWlnaHQgPiAwKSkgcmV0dXJuIFwiXCI7XG4gICAgaWYgKGlubmVyV2lkdGggLSBvdXRlcldpZHRoID4gUkVTSVpFX01FVFJJQ19TTEFDSyB8fCBpbm5lckhlaWdodCAtIG91dGVySGVpZ2h0ID4gUkVTSVpFX01FVFJJQ19TTEFDSykgcmV0dXJuIFwiXCI7XG4gICAgLy8gVGhlIHBvaW50ZXIgaXMgYWxyZWFkeSBvdXRzaWRlIHRoZSByZXBvcnRlZCB3aW5kb3cuIFRob3NlIG1ldHJpY3MgYmVsb25nXG4gICAgLy8gdG8gdGhlIHJlc3RvcmVkIHNpemUsIG5vdCB0aGUgbWF4aW1pc2VkIHBhZ2UuXG4gICAgaWYgKHggPiBvdXRlcldpZHRoIHx8IHkgPiBvdXRlckhlaWdodCkgcmV0dXJuIFwiXCI7XG5cbiAgICBjb25zdCByaWdodCA9IG91dGVyV2lkdGggLSB4IDwgYm9yZGVyO1xuICAgIGNvbnN0IGxlZnQgPSB4IDwgYm9yZGVyO1xuICAgIGNvbnN0IHRvcCA9IHkgPCBib3JkZXI7XG4gICAgY29uc3QgYm90dG9tID0gb3V0ZXJIZWlnaHQgLSB5IDwgYm9yZGVyO1xuICAgIGlmIChyaWdodCAmJiBib3R0b20pIHJldHVybiBSRVNJWkVfQ1VSU09SUy5zZTtcbiAgICBpZiAobGVmdCAmJiBib3R0b20pIHJldHVybiBSRVNJWkVfQ1VSU09SUy5zdztcbiAgICBpZiAobGVmdCAmJiB0b3ApIHJldHVybiBSRVNJWkVfQ1VSU09SUy5udztcbiAgICBpZiAodG9wICYmIHJpZ2h0KSByZXR1cm4gUkVTSVpFX0NVUlNPUlMubmU7XG4gICAgaWYgKGxlZnQpIHJldHVybiBSRVNJWkVfQ1VSU09SUy53O1xuICAgIGlmICh0b3ApIHJldHVybiBSRVNJWkVfQ1VSU09SUy5uO1xuICAgIGlmIChib3R0b20pIHJldHVybiBSRVNJWkVfQ1VSU09SUy5zO1xuICAgIGlmIChyaWdodCkgcmV0dXJuIFJFU0laRV9DVVJTT1JTLmU7XG4gICAgcmV0dXJuIFwiXCI7XG59XG4iLCAiLypcbiBfXHQgICBfX1x0ICBfIF9fXG58IHxcdCAvIC9fX18gXyhfKSAvX19fX1xufCB8IC98IC8gLyBfXyBgLyAvIC8gX19fL1xufCB8LyB8LyAvIC9fLyAvIC8gKF9fICApXG58X18vfF9fL1xcX18sXy9fL18vX19fXy9cblRoZSBlbGVjdHJvbiBhbHRlcm5hdGl2ZSBmb3IgR29cbihjKSBMZWEgQW50aG9ueSAyMDE5LXByZXNlbnRcbiovXG4vKiBqc2hpbnQgZXN2ZXJzaW9uOiA5ICovXG5pbXBvcnQgKiBhcyBMb2cgZnJvbSAnLi9sb2cnO1xuaW1wb3J0IHtcbiAgZXZlbnRMaXN0ZW5lcnMsXG4gIEV2ZW50c0VtaXQsXG4gIEV2ZW50c05vdGlmeSxcbiAgRXZlbnRzT2ZmLFxuICBFdmVudHNPZmZBbGwsXG4gIEV2ZW50c09uLFxuICBFdmVudHNPbmNlLFxuICBFdmVudHNPbk11bHRpcGxlLFxufSBmcm9tIFwiLi9ldmVudHNcIjtcbmltcG9ydCB7IENhbGwsIENhbGxiYWNrLCBjYWxsYmFja3MgfSBmcm9tICcuL2NhbGxzJztcbmltcG9ydCB7IFNldEJpbmRpbmdzIH0gZnJvbSBcIi4vYmluZGluZ3NcIjtcbmltcG9ydCAqIGFzIFdpbmRvdyBmcm9tIFwiLi93aW5kb3dcIjtcbmltcG9ydCAqIGFzIFNjcmVlbiBmcm9tIFwiLi9zY3JlZW5cIjtcbmltcG9ydCAqIGFzIEJyb3dzZXIgZnJvbSBcIi4vYnJvd3NlclwiO1xuaW1wb3J0ICogYXMgQ2xpcGJvYXJkIGZyb20gXCIuL2NsaXBib2FyZFwiO1xuaW1wb3J0ICogYXMgRHJhZ0FuZERyb3AgZnJvbSBcIi4vZHJhZ2FuZGRyb3BcIjtcbmltcG9ydCAqIGFzIENvbnRleHRNZW51IGZyb20gXCIuL2NvbnRleHRtZW51XCI7XG5pbXBvcnQgeyBpc1Jlc2l6ZUN1cnNvciwgcmVzaXplQ3Vyc29yRm9yUG9pbnRlciB9IGZyb20gXCIuL3Jlc2l6ZV9jdXJzb3JcIjtcblxuZXhwb3J0IGZ1bmN0aW9uIFF1aXQoKSB7XG4gICAgd2luZG93LldhaWxzSW52b2tlKCdRJyk7XG59XG5cbmV4cG9ydCBmdW5jdGlvbiBTaG93KCkge1xuICAgIHdpbmRvdy5XYWlsc0ludm9rZSgnUycpO1xufVxuXG5leHBvcnQgZnVuY3Rpb24gSGlkZSgpIHtcbiAgICB3aW5kb3cuV2FpbHNJbnZva2UoJ0gnKTtcbn1cblxuZXhwb3J0IGZ1bmN0aW9uIEVudmlyb25tZW50KCkge1xuICAgIHJldHVybiBDYWxsKFwiOndhaWxzOkVudmlyb25tZW50XCIpO1xufVxuXG4vLyBUaGUgSlMgcnVudGltZVxud2luZG93LnJ1bnRpbWUgPSB7XG4gICAgLi4uTG9nLFxuICAgIC4uLldpbmRvdyxcbiAgICAuLi5Ccm93c2VyLFxuICAgIC4uLlNjcmVlbixcbiAgICAuLi5DbGlwYm9hcmQsXG4gICAgLi4uRHJhZ0FuZERyb3AsXG4gICAgRXZlbnRzT24sXG4gICAgRXZlbnRzT25jZSxcbiAgICBFdmVudHNPbk11bHRpcGxlLFxuICAgIEV2ZW50c0VtaXQsXG4gICAgRXZlbnRzT2ZmLFxuICAgIEV2ZW50c09mZkFsbCxcbiAgICBFbnZpcm9ubWVudCxcbiAgICBTaG93LFxuICAgIEhpZGUsXG4gICAgUXVpdFxufTtcblxuLy8gSW50ZXJuYWwgd2FpbHMgZW5kcG9pbnRzXG53aW5kb3cud2FpbHMgPSB7XG4gICAgQ2FsbGJhY2ssXG4gICAgRXZlbnRzTm90aWZ5LFxuICAgIFNldEJpbmRpbmdzLFxuICAgIGV2ZW50TGlzdGVuZXJzLFxuICAgIGNhbGxiYWNrcyxcbiAgICBmbGFnczoge1xuICAgICAgICBkaXNhYmxlU2Nyb2xsYmFyRHJhZzogZmFsc2UsXG4gICAgICAgIGRpc2FibGVEZWZhdWx0Q29udGV4dE1lbnU6IGZhbHNlLFxuICAgICAgICBlbmFibGVSZXNpemU6IGZhbHNlLFxuICAgICAgICBkZWZhdWx0Q3Vyc29yOiBudWxsLFxuICAgICAgICBib3JkZXJUaGlja25lc3M6IDYsXG4gICAgICAgIHNob3VsZERyYWc6IGZhbHNlLFxuICAgICAgICBkZWZlckRyYWdUb01vdXNlTW92ZTogdHJ1ZSxcbiAgICAgICAgY3NzRHJhZ1Byb3BlcnR5OiBcIi0td2FpbHMtZHJhZ2dhYmxlXCIsXG4gICAgICAgIGNzc0RyYWdWYWx1ZTogXCJkcmFnXCIsXG4gICAgICAgIGNzc0Ryb3BQcm9wZXJ0eTogXCItLXdhaWxzLWRyb3AtdGFyZ2V0XCIsXG4gICAgICAgIGNzc0Ryb3BWYWx1ZTogXCJkcm9wXCIsXG4gICAgICAgIGVuYWJsZVdhaWxzRHJhZ0FuZERyb3A6IGZhbHNlLFxuICAgIH1cbn07XG5cbi8vIFNldCB0aGUgYmluZGluZ3NcbmlmICh3aW5kb3cud2FpbHNiaW5kaW5ncykge1xuICAgIHdpbmRvdy53YWlscy5TZXRCaW5kaW5ncyh3aW5kb3cud2FpbHNiaW5kaW5ncyk7XG4gICAgZGVsZXRlIHdpbmRvdy53YWlscy5TZXRCaW5kaW5ncztcbn1cblxuLy8gKGJvb2wpIFRoaXMgaXMgZXZhbHVhdGVkIGF0IGJ1aWxkIHRpbWUgaW4gcGFja2FnZS5qc29uXG5pZiAoIURFQlVHKSB7XG4gICAgZGVsZXRlIHdpbmRvdy53YWlsc2JpbmRpbmdzO1xufVxuXG5sZXQgZHJhZ1Rlc3QgPSBmdW5jdGlvbihlKSB7XG4gICAgdmFyIHZhbCA9IHdpbmRvdy5nZXRDb21wdXRlZFN0eWxlKGUudGFyZ2V0KS5nZXRQcm9wZXJ0eVZhbHVlKHdpbmRvdy53YWlscy5mbGFncy5jc3NEcmFnUHJvcGVydHkpO1xuICAgIGlmICh2YWwpIHtcbiAgICAgICAgdmFsID0gdmFsLnRyaW0oKTtcbiAgICB9XG5cbiAgICBpZiAodmFsICE9PSB3aW5kb3cud2FpbHMuZmxhZ3MuY3NzRHJhZ1ZhbHVlKSB7XG4gICAgICAgIHJldHVybiBmYWxzZTtcbiAgICB9XG5cbiAgICBpZiAoZS5idXR0b25zICE9PSAxKSB7XG4gICAgICAgIC8vIERvIG5vdCBzdGFydCBkcmFnZ2luZyBpZiBub3QgdGhlIHByaW1hcnkgYnV0dG9uIGhhcyBiZWVuIGNsaWNrZWQuXG4gICAgICAgIHJldHVybiBmYWxzZTtcbiAgICB9XG5cbiAgICBpZiAoZS5kZXRhaWwgIT09IDEpIHtcbiAgICAgICAgLy8gRG8gbm90IHN0YXJ0IGRyYWdnaW5nIGlmIG1vcmUgdGhhbiBvbmNlIGhhcyBiZWVuIGNsaWNrZWQsIGUuZy4gd2hlbiBkb3VibGUgY2xpY2tpbmdcbiAgICAgICAgcmV0dXJuIGZhbHNlO1xuICAgIH1cblxuICAgIHJldHVybiB0cnVlO1xufTtcblxud2luZG93LndhaWxzLnNldENTU0RyYWdQcm9wZXJ0aWVzID0gZnVuY3Rpb24ocHJvcGVydHksIHZhbHVlKSB7XG4gICAgd2luZG93LndhaWxzLmZsYWdzLmNzc0RyYWdQcm9wZXJ0eSA9IHByb3BlcnR5O1xuICAgIHdpbmRvdy53YWlscy5mbGFncy5jc3NEcmFnVmFsdWUgPSB2YWx1ZTtcbn1cblxud2luZG93LndhaWxzLnNldENTU0Ryb3BQcm9wZXJ0aWVzID0gZnVuY3Rpb24ocHJvcGVydHksIHZhbHVlKSB7XG4gICAgd2luZG93LndhaWxzLmZsYWdzLmNzc0Ryb3BQcm9wZXJ0eSA9IHByb3BlcnR5O1xuICAgIHdpbmRvdy53YWlscy5mbGFncy5jc3NEcm9wVmFsdWUgPSB2YWx1ZTtcbn1cblxud2luZG93LmFkZEV2ZW50TGlzdGVuZXIoJ21vdXNlZG93bicsIChlKSA9PiB7XG4gICAgLy8gQ2hlY2sgZm9yIHJlc2l6aW5nXG4gICAgaWYgKHdpbmRvdy53YWlscy5mbGFncy5yZXNpemVFZGdlKSB7XG4gICAgICAgIHdpbmRvdy5XYWlsc0ludm9rZShcInJlc2l6ZTpcIiArIHdpbmRvdy53YWlscy5mbGFncy5yZXNpemVFZGdlKTtcbiAgICAgICAgZS5wcmV2ZW50RGVmYXVsdCgpO1xuICAgICAgICByZXR1cm47XG4gICAgfVxuXG4gICAgaWYgKGRyYWdUZXN0KGUpKSB7XG4gICAgICAgIGlmICh3aW5kb3cud2FpbHMuZmxhZ3MuZGlzYWJsZVNjcm9sbGJhckRyYWcpIHtcbiAgICAgICAgICAgIC8vIFRoaXMgY2hlY2tzIGZvciBjbGlja3Mgb24gdGhlIHNjcm9sbCBiYXJcbiAgICAgICAgICAgIGlmIChlLm9mZnNldFggPiBlLnRhcmdldC5jbGllbnRXaWR0aCB8fCBlLm9mZnNldFkgPiBlLnRhcmdldC5jbGllbnRIZWlnaHQpIHtcbiAgICAgICAgICAgICAgICByZXR1cm47XG4gICAgICAgICAgICB9XG4gICAgICAgIH1cbiAgICAgICAgaWYgKHdpbmRvdy53YWlscy5mbGFncy5kZWZlckRyYWdUb01vdXNlTW92ZSkge1xuICAgICAgICAgICAgd2luZG93LndhaWxzLmZsYWdzLnNob3VsZERyYWcgPSB0cnVlO1xuICAgICAgICB9IGVsc2Uge1xuICAgICAgICAgICAgZS5wcmV2ZW50RGVmYXVsdCgpXG4gICAgICAgICAgICB3aW5kb3cuV2FpbHNJbnZva2UoXCJkcmFnXCIpO1xuICAgICAgICB9XG4gICAgICAgIHJldHVybjtcbiAgICB9IGVsc2Uge1xuICAgICAgICB3aW5kb3cud2FpbHMuZmxhZ3Muc2hvdWxkRHJhZyA9IGZhbHNlO1xuICAgIH1cbn0pO1xuXG53aW5kb3cuYWRkRXZlbnRMaXN0ZW5lcignbW91c2V1cCcsICgpID0+IHtcbiAgICB3aW5kb3cud2FpbHMuZmxhZ3Muc2hvdWxkRHJhZyA9IGZhbHNlO1xufSk7XG5cbmZ1bmN0aW9uIHJlc3RvcmVkQ3Vyc29yKCkge1xuICAgIGNvbnN0IGZhbGxiYWNrID0gd2luZG93LndhaWxzLmZsYWdzLmRlZmF1bHRDdXJzb3I7XG4gICAgcmV0dXJuIGZhbGxiYWNrICYmICFpc1Jlc2l6ZUN1cnNvcihmYWxsYmFjaykgPyBmYWxsYmFjayA6IFwiXCI7XG59XG5cbmZ1bmN0aW9uIHNldFJlc2l6ZShjdXJzb3IpIHtcbiAgICBpZiAod2luZG93LndhaWxzLmZsYWdzLnJlc2l6ZUVkZ2UgPT09IGN1cnNvcikgcmV0dXJuO1xuICAgIGRvY3VtZW50LmRvY3VtZW50RWxlbWVudC5zdHlsZS5jdXJzb3IgPSBjdXJzb3IgfHwgcmVzdG9yZWRDdXJzb3IoKTtcbiAgICB3aW5kb3cud2FpbHMuZmxhZ3MucmVzaXplRWRnZSA9IGN1cnNvcjtcbn1cblxuLy8gY2xlYXJSZXNpemVDdXJzb3IgZHJvcHMgYSBzaXppbmcgY3Vyc29yIGxlZnQgb24gdGhlIHJvb3QgZWxlbWVudC4gQVxuLy8gbWF4aW1pc2VkIG9yIGZ1bGxzY3JlZW4gb3V0ZXIgd2luZG93IGNhbm5vdCBiZSByZXNpemVkLCBzbyB0aGF0IGN1cnNvciBpcyBzdGFsZS5cbmZ1bmN0aW9uIGNsZWFyUmVzaXplQ3Vyc29yKCkge1xuICAgIGNvbnN0IGN1cnJlbnQgPSBkb2N1bWVudC5kb2N1bWVudEVsZW1lbnQuc3R5bGUuY3Vyc29yIHx8IFwiXCI7XG4gICAgaWYgKCF3aW5kb3cud2FpbHMuZmxhZ3MucmVzaXplRWRnZSAmJiAhaXNSZXNpemVDdXJzb3IoY3VycmVudCkpIHJldHVybjtcbiAgICBkb2N1bWVudC5kb2N1bWVudEVsZW1lbnQuc3R5bGUuY3Vyc29yID0gcmVzdG9yZWRDdXJzb3IoKTtcbiAgICB3aW5kb3cud2FpbHMuZmxhZ3MucmVzaXplRWRnZSA9IHVuZGVmaW5lZDtcbn1cblxuLy8gQ2FsbGVkIGFmdGVyIHRoZSBmdWxsc2NyZWVuIHNpemUgY2hhbmdlLiBFZGdlIHRyYWNraW5nIHN0YXlzIG9mZiB1bnRpbFxuLy8gdW5mdWxsc2NyZWVuIHR1cm5zIGl0IGJhY2sgb24sIGFuZCBhIHNpemluZyBjdXJzb3IgcGFpbnRlZCBkdXJpbmcgdGhlXG4vLyByZXNpemUgaXMgcmVtb3ZlZCBoZXJlIGluc3RlYWQgb2Ygd2FpdGluZyBmb3IgdGhlIG5leHQgbW91c2Vtb3ZlLlxud2luZG93LndhaWxzLnN1c3BlbmRSZXNpemVDdXJzb3IgPSBmdW5jdGlvbigpIHtcbiAgICB3aW5kb3cud2FpbHMuZmxhZ3MuZW5hYmxlUmVzaXplID0gZmFsc2U7XG4gICAgY2xlYXJSZXNpemVDdXJzb3IoKTtcbn07XG5cbndpbmRvdy5hZGRFdmVudExpc3RlbmVyKCdtb3VzZW1vdmUnLCBmdW5jdGlvbihlKSB7XG4gICAgaWYgKHdpbmRvdy53YWlscy5mbGFncy5zaG91bGREcmFnKSB7XG4gICAgICAgIHdpbmRvdy53YWlscy5mbGFncy5zaG91bGREcmFnID0gZmFsc2U7XG4gICAgICAgIGxldCBtb3VzZVByZXNzZWQgPSBlLmJ1dHRvbnMgIT09IHVuZGVmaW5lZCA/IGUuYnV0dG9ucyA6IGUud2hpY2g7XG4gICAgICAgIGlmIChtb3VzZVByZXNzZWQgPiAwKSB7XG4gICAgICAgICAgICB3aW5kb3cuV2FpbHNJbnZva2UoXCJkcmFnXCIpO1xuICAgICAgICAgICAgcmV0dXJuO1xuICAgICAgICB9XG4gICAgfVxuICAgIGlmICghd2luZG93LndhaWxzLmZsYWdzLmVuYWJsZVJlc2l6ZSkge1xuICAgICAgICAvLyBGdWxsc2NyZWVuIGxlYXZlcyBlZGdlIHRyYWNraW5nIG9mZi4gT25seSBhIGN1cnNvciBhbHJlYWR5IGxhdGNoZWRcbiAgICAgICAgLy8gb24gdGhlIHJvb3QgbmVlZHMgY2xlYXJpbmc7IHJlYWRpbmcgc3R5bGUgb24gZXZlcnkgbW92ZSBkb2VzIG5vdC5cbiAgICAgICAgaWYgKHdpbmRvdy53YWlscy5mbGFncy5yZXNpemVFZGdlICE9PSB1bmRlZmluZWQpIGNsZWFyUmVzaXplQ3Vyc29yKCk7XG4gICAgICAgIHJldHVybjtcbiAgICB9XG4gICAgaWYgKHdpbmRvdy53YWlscy5mbGFncy5kZWZhdWx0Q3Vyc29yID09IG51bGwpIHtcbiAgICAgICAgY29uc3QgY3VycmVudCA9IGRvY3VtZW50LmRvY3VtZW50RWxlbWVudC5zdHlsZS5jdXJzb3IgfHwgXCJcIjtcbiAgICAgICAgd2luZG93LndhaWxzLmZsYWdzLmRlZmF1bHRDdXJzb3IgPSBpc1Jlc2l6ZUN1cnNvcihjdXJyZW50KSA/IFwiXCIgOiBjdXJyZW50O1xuICAgIH1cbiAgICBjb25zdCBjdXJzb3IgPSByZXNpemVDdXJzb3JGb3JQb2ludGVyKFxuICAgICAgICB3aW5kb3cub3V0ZXJXaWR0aCxcbiAgICAgICAgd2luZG93Lm91dGVySGVpZ2h0LFxuICAgICAgICB3aW5kb3cuaW5uZXJXaWR0aCxcbiAgICAgICAgd2luZG93LmlubmVySGVpZ2h0LFxuICAgICAgICBlLmNsaWVudFgsXG4gICAgICAgIGUuY2xpZW50WSxcbiAgICAgICAgd2luZG93LndhaWxzLmZsYWdzLmJvcmRlclRoaWNrbmVzcyxcbiAgICApO1xuICAgIGlmICghY3Vyc29yKSB7XG4gICAgICAgIGlmICh3aW5kb3cud2FpbHMuZmxhZ3MucmVzaXplRWRnZSAhPT0gdW5kZWZpbmVkKSBjbGVhclJlc2l6ZUN1cnNvcigpO1xuICAgICAgICByZXR1cm47XG4gICAgfVxuICAgIHNldFJlc2l6ZShjdXJzb3IpO1xufSk7XG5cbi8vIFNldHVwIGNvbnRleHQgbWVudSBob29rXG53aW5kb3cuYWRkRXZlbnRMaXN0ZW5lcignY29udGV4dG1lbnUnLCBmdW5jdGlvbihlKSB7XG4gICAgLy8gYWx3YXlzIHNob3cgdGhlIGNvbnRleHRtZW51IGluIGRlYnVnICYgZGV2XG4gICAgaWYgKERFQlVHKSByZXR1cm47XG5cbiAgICBpZiAod2luZG93LndhaWxzLmZsYWdzLmRpc2FibGVEZWZhdWx0Q29udGV4dE1lbnUpIHtcbiAgICAgICAgZS5wcmV2ZW50RGVmYXVsdCgpO1xuICAgIH0gZWxzZSB7XG4gICAgICAgIENvbnRleHRNZW51LnByb2Nlc3NEZWZhdWx0Q29udGV4dE1lbnUoZSk7XG4gICAgfVxufSk7XG5cbndpbmRvdy5XYWlsc0ludm9rZShcInJ1bnRpbWU6cmVhZHlcIik7Il0sCiAgIm1hcHBpbmdzIjogIjs7Ozs7Ozs7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFrQkEsV0FBUyxlQUFlLE9BQU8sU0FBUztBQUl2QyxXQUFPLFlBQVksTUFBTSxRQUFRLE9BQU87QUFBQSxFQUN6QztBQVFPLFdBQVMsU0FBUyxTQUFTO0FBQ2pDLG1CQUFlLEtBQUssT0FBTztBQUFBLEVBQzVCO0FBUU8sV0FBUyxTQUFTLFNBQVM7QUFDakMsbUJBQWUsS0FBSyxPQUFPO0FBQUEsRUFDNUI7QUFRTyxXQUFTLFNBQVMsU0FBUztBQUNqQyxtQkFBZSxLQUFLLE9BQU87QUFBQSxFQUM1QjtBQVFPLFdBQVMsUUFBUSxTQUFTO0FBQ2hDLG1CQUFlLEtBQUssT0FBTztBQUFBLEVBQzVCO0FBUU8sV0FBUyxXQUFXLFNBQVM7QUFDbkMsbUJBQWUsS0FBSyxPQUFPO0FBQUEsRUFDNUI7QUFRTyxXQUFTLFNBQVMsU0FBUztBQUNqQyxtQkFBZSxLQUFLLE9BQU87QUFBQSxFQUM1QjtBQVFPLFdBQVMsU0FBUyxTQUFTO0FBQ2pDLG1CQUFlLEtBQUssT0FBTztBQUFBLEVBQzVCO0FBUU8sV0FBUyxZQUFZLFVBQVU7QUFDckMsbUJBQWUsS0FBSyxRQUFRO0FBQUEsRUFDN0I7QUFHTyxNQUFNLFdBQVc7QUFBQSxJQUN2QixPQUFPO0FBQUEsSUFDUCxPQUFPO0FBQUEsSUFDUCxNQUFNO0FBQUEsSUFDTixTQUFTO0FBQUEsSUFDVCxPQUFPO0FBQUEsRUFDUjs7O0FDOUZB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
