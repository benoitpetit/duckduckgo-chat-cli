(() => {
  "use strict";

  const PREFIX = "#duckchat-relay=";
  const CHANNEL = "duckchat-cli-relay-main";
  let initialized = false;

  function startRelayFromFragment() {
    if (initialized) return;
    const fragment = window.location.hash;
    if (!fragment.startsWith(PREFIX)) return;

    const parameters = new URLSearchParams(fragment.slice(PREFIX.length));
    startRelay(parameters.get("endpoint"), parameters.get("token"));
  }

  function startRelay(endpoint, token) {
    if (initialized) return;
    let relayURL;
    try {
      relayURL = new URL(endpoint);
    } catch {
      return;
    }
    if (
      relayURL.protocol !== "http:" ||
      relayURL.hostname !== "127.0.0.1" ||
      (relayURL.pathname !== "/" && relayURL.pathname !== "") ||
      relayURL.username ||
      relayURL.password ||
      relayURL.search ||
      relayURL.hash ||
      !relayURL.port ||
      !/^[A-Za-z0-9_-]{40,60}$/.test(token || "")
    ) {
      return;
    }

    const relayEndpoint = relayURL.origin;
    initialized = true;
    // Keep the fragment for the one reload after Duck.ai site data is reset.
    // URL fragments are never sent in HTTP requests to Duck.ai.

    let polling = true;
    let pollInFlight = false;
    let activeRequestID = "";
    let completed = false;
    let interval;

    function onMainWorldMessage(event) {
      if (event.source !== window || event.origin !== window.location.origin) return;
      const message = event.data;
      if (!message || message.source !== CHANNEL || !message.frame || !activeRequestID) return;
      if (message.frame.id !== activeRequestID) return;

      chrome.runtime.sendMessage(
        { type: "duckchat-relay:frame", endpoint: relayEndpoint, token, frame: message.frame },
        (result) => {
          const runtimeError = chrome.runtime.lastError;
          window.postMessage(
            { source: CHANNEL, ack: message.sequence, ok: !runtimeError && result?.ok === true },
            window.location.origin,
          );
          if (message.frame.type === "finish" || message.frame.type === "error") {
            completed = true;
            polling = false;
            if (interval) window.clearInterval(interval);
            window.removeEventListener("message", onMainWorldMessage);
          }
        },
      );
    }

    window.addEventListener("message", onMainWorldMessage);
    window.addEventListener("pagehide", () => {
      if (!activeRequestID || completed) return;
      chrome.runtime.sendMessage({
        type: "duckchat-relay:frame",
        endpoint: relayEndpoint,
        token,
        frame: { type: "error", id: activeRequestID, message: "The Duck.ai browser tab closed during retry" },
      });
    }, { once: true });

    function poll() {
      if (!polling || pollInFlight) return;
      pollInFlight = true;
      chrome.runtime.sendMessage(
        { type: "duckchat-relay:next", endpoint: relayEndpoint, token },
        (result) => {
          pollInFlight = false;
          if (result?.reload === true) {
            polling = false;
            if (interval) window.clearInterval(interval);
            try { window.sessionStorage.clear(); } catch { /* Reload still resets the page. */ }
            window.location.reload();
            return;
          }
          if (chrome.runtime.lastError || !result?.job) return;
          const job = result.job;
          if (!/^[A-Za-z0-9_-]{40,60}$/.test(job.id || "")) return;
          activeRequestID = job.id;
          polling = false;
          if (interval) window.clearInterval(interval);
          window.history.replaceState(null, "", window.location.pathname + window.location.search);
          chrome.runtime.sendMessage(
            { type: "duckchat-relay:execute", endpoint: relayEndpoint, token, job },
            (executeResult) => {
              if (chrome.runtime.lastError || executeResult?.ok !== true) {
                chrome.runtime.sendMessage({
                  type: "duckchat-relay:frame",
                  endpoint: relayEndpoint,
                  token,
                  frame: { type: "error", id: job.id, message: "Chrome could not start the Duck.ai page request" },
                });
              }
            },
          );
        },
      );
    }

    // Polling with independent messages also lets the MV3 worker sleep safely.
    poll();
    interval = window.setInterval(poll, 250);
    window.setTimeout(() => {
      polling = false;
      if (interval) window.clearInterval(interval);
      if (!activeRequestID) window.removeEventListener("message", onMainWorldMessage);
    }, 18_000);
  }

  window.addEventListener("hashchange", startRelayFromFragment);
  startRelayFromFragment();
})();
