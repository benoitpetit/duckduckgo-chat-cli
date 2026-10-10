"use strict";

const sessions = new Map();

chrome.tabs.onRemoved.addListener((tabId) => {
  sessions.delete(tabId);
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (!isDuckAITopFrame(sender)) return false;

  if (message?.type === "duckchat-relay:next") {
    void takeNextJob(message, sender.tab.id).then(sendResponse, () => sendResponse({}));
    return true;
  }

  if (message?.type === "duckchat-relay:execute") {
    void executeJob(message, sender.tab.id).then(sendResponse, () => sendResponse({ ok: false }));
    return true;
  }

  if (message?.type === "duckchat-relay:frame") {
    void forwardFrame(message, sender.tab.id).then(sendResponse, () => sendResponse({ ok: false }));
    return true;
  }

  return false;
});

function isDuckAITopFrame(sender) {
  if (!sender.tab || sender.frameId !== 0 || !sender.url) return false;
  try {
    return new URL(sender.url).origin === "https://duck.ai";
  } catch {
    return false;
  }
}

function validRelay(endpoint, token) {
  let parsed;
  try {
    parsed = new URL(endpoint);
  } catch {
    return false;
  }
  return parsed.protocol === "http:" && parsed.hostname === "127.0.0.1" && Boolean(parsed.port) &&
    !parsed.username && !parsed.password && (parsed.pathname === "/" || parsed.pathname === "") && !parsed.search && !parsed.hash &&
    /^[A-Za-z0-9_-]{40,60}$/.test(token || "");
}

function getOrCreateSession(message, tabId) {
  if (!validRelay(message.endpoint, message.token)) throw new Error("invalid relay session");
  let session = sessions.get(tabId);
  if (session) {
    if (session.endpoint !== message.endpoint || session.token !== message.token) {
      throw new Error("another relay is already active in this tab");
    }
    return session;
  }
  session = { endpoint: message.endpoint, token: message.token, requestID: "", busy: false, job: null, resetDone: false };
  sessions.set(tabId, session);
  return session;
}

async function takeNextJob(message, tabId) {
  const session = getOrCreateSession(message, tabId);
  if (session.job && session.resetDone && !session.busy) {
    session.busy = true;
    return { job: session.job };
  }
  if (session.job || session.busy) return {};
  const response = await fetch(`${session.endpoint}/v1/next`, {
    method: "GET",
    headers: { Authorization: `Bearer ${session.token}` },
    credentials: "omit",
    cache: "no-store",
  });
  if (response.status === 204) return { empty: true };
  if (!response.ok) throw new Error("relay did not provide a request");
  const job = await response.json();
  if (
    job?.method !== "POST" ||
    typeof job.id !== "string" || !/^[A-Za-z0-9_-]{40,60}$/.test(job.id) ||
    typeof job.body !== "string" || job.body.length > 32 * 1024 * 1024 ||
    !job.headers || typeof job.headers !== "object" ||
    !validChatURL(job.url)
  ) {
    throw new Error("relay request was not valid");
  }
  session.requestID = job.id;
  session.job = job;
  try {
    // Match Chrome's "Clear site data" for Duck.ai only. This clears the
    // stale anonymous session before the page computes a new request proof.
    await chrome.browsingData.remove({ origins: ["https://duck.ai"] }, {
      cache: true,
      cacheStorage: true,
      cookies: true,
      indexedDB: true,
      localStorage: true,
      serviceWorkers: true,
    });
    session.resetDone = true;
    return { reload: true };
  } catch {
    session.resetDone = true;
    session.busy = true;
    return { job, resetFailed: true };
  }
}

function validChatURL(value) {
  try {
    const parsed = new URL(value);
    return parsed.origin === "https://duck.ai" && parsed.protocol === "https:";
  } catch {
    return false;
  }
}

async function executeJob(message, tabId) {
  let session = sessions.get(tabId);
  const job = message.job;
  if (!session && validRelay(message.endpoint, message.token)) {
    session = getOrCreateSession(message, tabId);
  }
  if (session && !session.busy && typeof job?.id === "string") {
    session.requestID = job.id;
    session.busy = true;
  }
  if (
    !session || session.endpoint !== message.endpoint || session.token !== message.token ||
    !session.busy || session.requestID !== job?.id
  ) {
    return { ok: false };
  }
  try {
    await chrome.scripting.executeScript({
      target: { tabId },
      world: "MAIN",
      func: runDuckAIRequest,
      args: [job],
    });
    return { ok: true };
  } catch {
    return { ok: false };
  }
}

function runDuckAIRequest(job) {
  const CHANNEL = "duckchat-cli-relay-main";
  const allowedHeaders = ["content-type", "retry-after", "x-vqd-4", "cache-control"];
  let sequence = 0;

  const sendFrame = (frame) => new Promise((resolve) => {
    const current = ++sequence;
    const timeout = window.setTimeout(() => {
      window.removeEventListener("message", onAck);
      resolve(false);
    }, 15_000);
    const onAck = (event) => {
      if (event.source !== window || event.origin !== window.location.origin) return;
      if (event.data?.source !== CHANNEL || event.data?.ack !== current) return;
      window.clearTimeout(timeout);
      window.removeEventListener("message", onAck);
      resolve(event.data.ok === true);
    };
    window.addEventListener("message", onAck);
    window.postMessage({ source: CHANNEL, sequence: current, frame }, window.location.origin);
  });

  const toBase64 = (bytes) => {
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += 0x8000) {
      binary += String.fromCharCode(...bytes.subarray(offset, Math.min(offset + 0x8000, bytes.length)));
    }
    return btoa(binary);
  };

  const capturePageProofHeaders = async () => {
    let textarea;
    const pageDeadline = Date.now() + 15_000;
    while (Date.now() < pageDeadline) {
      const pageText = (document.body?.innerText || "").toLowerCase();
      if (pageText.includes("too many requests") || pageText.includes("trop de requêtes") ||
          pageText.includes("trop de requetes") || pageText.includes("err_rate_limit")) {
        throw new Error("Duck.ai is still rate limiting the browser page after site data reset");
      }
      textarea = document.querySelector('textarea[name="user-prompt"]');
      if (textarea && textarea.getClientRects().length) break;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    if (!textarea || !textarea.getClientRects().length) {
      throw new Error("Duck.ai prompt is unavailable after site data reset");
    }

    const originalFetch = window.fetch;
    let settle;
    const captured = new Promise((resolve) => { settle = resolve; });
    let intercepted = false;
    window.fetch = function (input, init) {
      let target;
      try { target = new URL(typeof input === "string" ? input : input?.url, window.location.href); } catch { /* Ignore other requests. */ }
      if (!intercepted && target?.origin === window.location.origin && target.pathname === "/duckchat/v1/chat") {
        intercepted = true;
        window.fetch = originalFetch;
        try {
          const request = new Request(input, init);
          settle(Object.fromEntries(request.headers.entries()));
        } catch {
          settle({});
        }
        return Promise.reject(new Error("DuckChat CLI intercepted its proof calibration request"));
      }
      return originalFetch.apply(window, arguments);
    };
    let captureTimeout;
    try {
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set;
      setter.call(textarea, `duckduckgo-chat-cli-${Date.now()}`);
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      const deadline = Date.now() + 10_000;
      let button;
      while (Date.now() < deadline) {
        button = document.querySelector('button[type="submit"]');
        if (button && !button.disabled) break;
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      if (!button || button.disabled) throw new Error("Duck.ai could not prepare a new proof");
      button.click();
      const pageHeaders = await Promise.race([
        captured,
        new Promise((_, reject) => {
          captureTimeout = window.setTimeout(() => reject(new Error("Duck.ai did not generate a new proof")), 10_000);
        }),
      ]);
      if (!pageHeaders["x-vqd-hash-1"]) throw new Error("Duck.ai generated no usable proof");
      return pageHeaders;
    } finally {
      if (captureTimeout) window.clearTimeout(captureTimeout);
      window.fetch = originalFetch;
    }
  };

  void (async () => {
    let requestTimeout;
    let heartbeatTimer;
    let heartbeatPending = false;
    let controller;
    try {
      const target = new URL(job.url);
      if (target.origin !== window.location.origin || target.protocol !== "https:") {
        throw new Error("request target does not match the Duck.ai page");
      }
      const freshProof = await capturePageProofHeaders();
      const headers = {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
      };
      for (const name of ["x-vqd-hash-1", "x-fe-signals", "x-fe-version", "x-ddg-journey-id"]) {
        if (freshProof[name]) headers[name] = freshProof[name];
      }
      controller = new AbortController();
      requestTimeout = window.setTimeout(() => controller.abort(), 10 * 60 * 1000);
      heartbeatTimer = window.setInterval(() => {
        if (heartbeatPending) return;
        heartbeatPending = true;
        void sendFrame({ type: "heartbeat", id: job.id })
          .then((ok) => { if (!ok) controller.abort(); })
          .finally(() => { heartbeatPending = false; });
      }, 5_000);
      const response = await fetch(target.href, {
        method: "POST",
        headers,
        body: job.body,
        credentials: "include",
        cache: "no-store",
        redirect: "error",
        signal: controller.signal,
      });
      const responseHeaders = {};
      for (const name of allowedHeaders) {
        const value = response.headers.get(name);
        if (value !== null) responseHeaders[name] = [value];
      }
      if (!await sendFrame({ type: "start", id: job.id, status: response.status, headers: responseHeaders })) {
        throw new Error("the CLI relay did not accept the browser response");
      }
      if (response.body) {
        const reader = response.body.getReader();
        while (true) {
          const result = await reader.read();
          if (result.done) break;
          for (let offset = 0; offset < result.value.length; offset += 48 * 1024) {
            const part = result.value.subarray(offset, Math.min(offset + 48 * 1024, result.value.length));
            if (!await sendFrame({ type: "chunk", id: job.id, data: toBase64(part) })) {
              await reader.cancel();
              throw new Error("the CLI stopped receiving the browser response");
            }
          }
        }
      }
      window.clearTimeout(requestTimeout);
      if (!await sendFrame({ type: "finish", id: job.id })) {
        throw new Error("the CLI relay could not finish the browser response");
      }
    } catch (error) {
      const safeReasons = new Set([
        "Duck.ai is still rate limiting the browser page after site data reset",
        "Duck.ai prompt is unavailable after site data reset",
        "Duck.ai could not prepare a new proof",
        "Duck.ai did not generate a new proof",
        "Duck.ai generated no usable proof",
      ]);
      const rawReason = String(error?.message || "");
      const redactedReason = rawReason
        .replace(/https?:\/\/\S+/g, "[URL]")
        .replace(/[A-Za-z0-9_-]{40,}/g, "[redacted]")
        .slice(0, 160);
      const reason = safeReasons.has(rawReason) ? rawReason :
        redactedReason ? `Chrome retry failed: ${redactedReason}` : "Chrome could not complete the Duck.ai browser retry";
      const frame = { type: "error", id: job.id, message: reason };
      // After a response has started, error also terminates its stream. Before
      // start, it lets the CLI return promptly instead of waiting for timeout.
      await sendFrame(frame);
    } finally {
      if (requestTimeout) window.clearTimeout(requestTimeout);
      if (heartbeatTimer) window.clearInterval(heartbeatTimer);
    }
  })();
  return true;
}

async function forwardFrame(message, tabId) {
  let session = sessions.get(tabId);
  const frame = message.frame;
  if (!session && validRelay(message.endpoint, message.token)) {
    session = getOrCreateSession(message, tabId);
  }
  if (session && !session.busy && typeof frame?.id === "string") {
    session.requestID = frame.id;
    session.busy = true;
  }
  if (
    !session || session.endpoint !== message.endpoint || session.token !== message.token ||
    !session.busy || frame?.id !== session.requestID ||
    !["start", "chunk", "finish", "error", "heartbeat"].includes(frame?.type)
  ) {
    return { ok: false };
  }

  const paths = { start: "/v1/start", chunk: "/v1/chunk", finish: "/v1/finish", error: "/v1/error", heartbeat: "/v1/ping" };
  try {
    const response = await fetch(`${session.endpoint}${paths[frame.type]}`, {
      method: frame.type === "heartbeat" ? "GET" : "POST",
      headers: {
        Authorization: `Bearer ${session.token}`,
        "Content-Type": "application/json",
      },
      body: frame.type === "heartbeat" ? undefined : JSON.stringify(frame),
      credentials: "omit",
      cache: "no-store",
    });
    if (!response.ok && response.status !== 204) throw new Error("relay rejected the browser response");
    if (frame.type === "finish" || frame.type === "error") sessions.delete(tabId);
    return { ok: true };
  } catch {
    sessions.delete(tabId);
    return { ok: false };
  }
}
