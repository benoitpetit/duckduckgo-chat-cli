"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

test("browser relay clears only Duck.ai site data before delivering the job", async () => {
  const source = fs.readFileSync(path.join(__dirname, "background.js"), "utf8");
  const calls = [];
  const job = {
    id: "b".repeat(43), url: "https://duck.ai/duckchat/v1/chat",
    method: "POST", headers: {}, body: "{}",
  };
  const context = vm.createContext({
    URL, Map,
    fetch: async () => {
      calls.push("fetch-job");
      return { status: 200, ok: true, json: async () => job };
    },
    chrome: {
      tabs: { onRemoved: { addListener() {} }, onUpdated: { addListener() {} } },
      runtime: { onMessage: { addListener() {} } },
      browsingData: { remove: async (options, data) => {
        calls.push("clear-site-data");
        assert.equal(JSON.stringify(options.origins), '["https://duck.ai"]');
        for (const kind of ["cache", "cacheStorage", "cookies", "indexedDB", "localStorage", "serviceWorkers"]) {
          assert.equal(data[kind], true, `${kind} must be cleared`);
        }
      } },
    },
  });
  vm.runInContext(source, context);
  const session = { endpoint: "http://127.0.0.1:7788", token: "a".repeat(43) };
  const first = await context.takeNextJob(session, 1);
  assert.equal(first.reload, true);
  const second = await context.takeNextJob(session, 1);
  assert.equal(second.job.id, job.id);
  assert.deepEqual(calls, ["fetch-job", "clear-site-data"]);
});

test("browser relay obtains the request proof from the cleared Duck.ai page", async () => {
  const source = fs.readFileSync(path.join(__dirname, "background.js"), "utf8");
  const frames = [];
  const nativeRequests = [];
  const listeners = new Set();
  let finish;
  const finished = new Promise((resolve) => { finish = resolve; });
  class Textarea {
    set value(next) { this.currentValue = next; }
    get value() { return this.currentValue; }
    dispatchEvent() {}
    getClientRects() { return [1]; }
  }
  const textarea = new Textarea();
  const window = {
    location: { origin: "https://duck.ai" },
    setTimeout, clearTimeout, setInterval, clearInterval,
    addEventListener(_type, listener) { listeners.add(listener); },
    removeEventListener(_type, listener) { listeners.delete(listener); },
    postMessage(message) {
      if (!message.frame) return;
      frames.push(message.frame);
      queueMicrotask(() => {
        for (const listener of [...listeners]) {
          listener({ source: window, origin: window.location.origin,
            data: { source: message.source, ack: message.sequence, ok: true } });
        }
        if (message.frame.type === "finish" || message.frame.type === "error") finish();
      });
    },
    fetch: async (url, options) => {
      nativeRequests.push({ url, options });
      return new Response('data: {"role":"assistant","message":"PONG"}\n\ndata: [DONE]\n\n', {
        status: 200, headers: { "content-type": "text/event-stream" },
      });
    },
  };
  const button = { disabled: false, click() {
    void window.fetch("https://duck.ai/duckchat/v1/chat", {
      method: "POST", headers: { "x-vqd-hash-1": "fresh-page-proof" }, body: "calibration",
    }).catch(() => {});
  } };
  const context = vm.createContext({
    window, URL, Request, Response, TextDecoder, AbortController, HTMLTextAreaElement: Textarea,
    fetch: (...args) => window.fetch(...args),
    Event, Date, Promise, setTimeout, clearTimeout, setInterval, clearInterval,
    btoa,
    document: { body: { innerText: "" }, querySelector(selector) {
      return selector.startsWith("textarea") ? textarea : button;
    } },
    chrome: {
      tabs: { onRemoved: { addListener() {} }, onUpdated: { addListener() {} } },
      runtime: { onMessage: { addListener() {} } },
    },
  });
  vm.runInContext(source, context);
  const job = {
    id: "b".repeat(43), url: "https://duck.ai/duckchat/v1/chat",
    method: "POST", headers: { "x-vqd-hash-1": "stale-proof" }, body: '{"messages":[]}',
  };
  assert.equal(context.runDuckAIRequest(job), true);
  let timeout;
  try {
    await Promise.race([finished, new Promise((_, reject) => {
      timeout = setTimeout(() => reject(new Error(`relay did not finish: ${JSON.stringify(frames)}`)), 1000);
    })]);
  } finally {
    clearTimeout(timeout);
  }
  assert.equal(nativeRequests.length, 1, "calibration must be intercepted before reaching Duck.ai");
  assert.equal(new Headers(nativeRequests[0].options.headers).get("x-vqd-hash-1"), "fresh-page-proof");
  assert.equal(nativeRequests[0].options.body, job.body);
  assert.deepEqual(frames.map((frame) => frame.type), ["start", "chunk", "finish"]);
  assert.deepEqual(Array.from(frames[0].headers["content-type"]), ["text/event-stream"]);
});
