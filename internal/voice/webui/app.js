"use strict";

const token = window.location.hash.slice(1);
if (window.location.hash) history.replaceState(null, "", window.location.pathname);

const ui = {
  connectionPill: document.querySelector("#connection-pill"),
  connection: document.querySelector("#connection-state"),
  controls: document.querySelector("#controls"),
  duck: document.querySelector("#duck-stage"),
  terminationPanel: document.querySelector("#termination-panel"),
  terminationMessage: document.querySelector("#termination-message"),
  subtitleSlot: document.querySelector("#subtitle-slot"),
  retry: document.querySelector("#retry-button"),
  closeVoice: document.querySelector("#close-voice-button"),
  voiceState: document.querySelector("#voice-state"),
  error: document.querySelector("#error-message"),
  start: document.querySelector("#start-button"),
  startLabel: document.querySelector("#start-button .button-label"),
  minimize: document.querySelector("#minimize-button"),
  mute: document.querySelector("#mute-button"),
  muteLabel: document.querySelector("#mute-button .button-label"),
  end: document.querySelector("#end-button"),
  endLabel: document.querySelector("#end-button .button-label"),
  audioEnable: document.querySelector("#audio-enable-button"),
  audio: document.querySelector("#remote-audio"),
  userSubtitle: document.querySelector("#user-subtitle-panel"),
  userTranscript: document.querySelector("#user-transcript"),
  assistantSubtitle: document.querySelector("#assistant-subtitle-panel"),
  assistantTranscript: document.querySelector("#assistant-transcript"),
};

let peer = null;
let channel = null;
let microphone = null;
let remoteAudioTrack = null;
let readinessSoundAttempt = 0;
let muted = false;
let ended = false;
let sessionAttempt = 0;
let hasAttemptedSessionStart = false;
let assistantOutputActive = false;
let subtitleHideTimer = null;
let captionTimer = null;
let captionTranscript = "";
let captionBuffer = "";
let captionQueue = [];
let captionCatchUpActive = false;
let inputTranscript = "";
let assistantResponseHasAudio = false;
let assistantTranscriptStarted = false;
let assistantAudioGateActive = false;
let assistantAudioGateTimer = null;

const MAX_CAPTION_WORDS = 11;
const MAX_CAPTION_CHARS = 92;
const CAPTION_WORDS_PER_SECOND = 2.9;

function wordCount(text) {
  return text.trim().split(/\s+/u).filter(Boolean).length;
}

function nextCaptionLength(force = false) {
  const sentenceEnd = /[.!?…]["'”’»)\]]*(?=\s|$)/u.exec(captionBuffer);
  if (sentenceEnd) {
    const length = sentenceEnd.index + sentenceEnd[0].length;
    if (wordCount(captionBuffer.slice(0, length)) <= MAX_CAPTION_WORDS) return length;
  }

  const words = [...captionBuffer.matchAll(/\S+/gu)];
  if (words.length > MAX_CAPTION_WORDS || captionBuffer.length > MAX_CAPTION_CHARS) {
    const target = Math.min(MAX_CAPTION_WORDS, words.length);
    if (target > 0) {
      const lastWord = words[target - 1];
      const end = lastWord.index + lastWord[0].length;
      if (end < captionBuffer.length || force) return end;
    }
    const charBoundary = captionBuffer.lastIndexOf(" ", MAX_CAPTION_CHARS);
    if (charBoundary > 0) return charBoundary;
  }

  return force && captionBuffer.trim() ? captionBuffer.length : 0;
}

function queueTranscriptCaptions(force = false) {
  while (captionBuffer.trim()) {
    const length = nextCaptionLength(force);
    if (!length) break;
    const text = captionBuffer.slice(0, length).trim();
    captionBuffer = captionBuffer.slice(length).trimStart();
    if (text) captionQueue.push({ text, words: wordCount(text) });
  }
  showNextAssistantCaption();
}

function captionDuration(caption) {
  const punctuationPause = /[.!?…]["'”’»)]*$/u.test(caption.text) ? 320 : 0;
  const estimated = (caption.words / CAPTION_WORDS_PER_SECOND) * 1000 + punctuationPause;
  return Math.max(1450, Math.min(4200, estimated));
}

function showNextAssistantCaption() {
  if (captionTimer || captionQueue.length === 0) return;
  if (!assistantOutputActive && !captionCatchUpActive) return;

  const caption = captionQueue.shift();
  setSubtitle("assistant", caption.text);
  captionTimer = setTimeout(() => {
    captionTimer = null;
    if (captionQueue.length > 0) {
      showNextAssistantCaption();
      return;
    }
    if (!assistantOutputActive) {
      captionCatchUpActive = false;
      scheduleSubtitleHide();
    }
  }, captionDuration(caption));
}

function setConnection(state, label) {
  ui.connectionPill.dataset.state = state;
  ui.connection.textContent = label;
}

function markVoiceConnectionReady() {
  const peerConnected = peer && peer.connectionState === "connected";
  const sessionChannelOpen = channel && channel.readyState === "open";
  if (!peerConnected || !sessionChannelOpen || ended) return false;

  setConnection("connected", "Connected");
  setDuck("listening");
  if (readinessSoundAttempt !== sessionAttempt) {
    readinessSoundAttempt = sessionAttempt;
    playReadinessChime();
  }
  showError("");
  return true;
}

function playReadinessChime(reverse = false) {
  const notes = reverse
    ? [{ frequency: 880, duration: 0.16 }, { frequency: 660, duration: 0.12 }]
    : [{ frequency: 660, duration: 0.12 }, { frequency: 880, duration: 0.16 }];
  playToneSequence(notes, 0.045);
}

function playToneSequence(notes, volume) {
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (typeof AudioContext !== "function") return;

  let context;
  try {
    context = new AudioContext();
    let startAt = context.currentTime + 0.02;

    for (const note of notes) {
      const oscillator = context.createOscillator();
      const gain = context.createGain();
      const endAt = startAt + note.duration;
      oscillator.type = "sine";
      oscillator.frequency.setValueAtTime(note.frequency, startAt);
      gain.gain.setValueAtTime(0.0001, startAt);
      gain.gain.exponentialRampToValueAtTime(volume, startAt + 0.015);
      gain.gain.exponentialRampToValueAtTime(0.0001, endAt);
      oscillator.connect(gain);
      gain.connect(context.destination);
      oscillator.start(startAt);
      oscillator.stop(endAt);
      startAt = endAt + 0.02;
    }

    window.setTimeout(() => { void context.close().catch(() => {}); }, 500);
  } catch (_) {
    if (context) void context.close().catch(() => {});
  }
}

function setDuck(state) {
  ui.duck.dataset.state = state;
  const labels = {
    idle: "Voice companion ready",
    connecting: "Connecting your audio…",
    listening: "Listening",
    "user-speaking": "Hearing you",
    thinking: "Thinking…",
    speaking: "Speaking",
    error: "Check the connection",
    "session-terminated": "Voice chat ended",
    ended: "Voice session ended",
  };
  const label = labels[state] || labels.idle;
  ui.voiceState.dataset.state = state;
  ui.voiceState.textContent = label;
  ui.duck.setAttribute("aria-label", `Duck.ai mascot. ${label}`);
}

function showTerminationPanel(message) {
  if (ended) return false;
  ended = true;
  sessionAttempt += 1;
  clearSubtitles();
  stopMedia();
  ui.controls.hidden = true;
  ui.subtitleSlot.hidden = true;
  ui.terminationPanel.hidden = false;
  ui.terminationMessage.textContent = message;
  showError("");
  setConnection("idle", "Voice chat ended");
  setDuck("session-terminated");
  return true;
}

function handleSessionTermination() {
  if (!showTerminationPanel("We ended this conversation due to inactivity. Would you like to try again?")) return;
  void request("/api/session-terminated", { method: "POST" }).catch(() => {});
}

function endActiveSession() {
  if (showTerminationPanel("Voice chat ended. Would you like to try again?")) {
    playReadinessChime(true);
  }
}

function showError(message) {
  ui.error.textContent = message;
  ui.error.hidden = !message;
}

function setSubtitle(speaker, text) {
  const isUser = speaker === "user";
  if (!isUser && subtitleHideTimer) {
    clearTimeout(subtitleHideTimer);
    subtitleHideTimer = null;
  }
  const panel = isUser ? ui.userSubtitle : ui.assistantSubtitle;
  const transcript = isUser ? ui.userTranscript : ui.assistantTranscript;
  panel.dataset.speaker = speaker;
  transcript.textContent = text;
  panel.hidden = !text.trim();
}

function showUserTranscript(transcript) {
  const text = transcript.trim();
  if (!text) return;
  inputTranscript = text;
  setSubtitle("user", text);
}

function appendUserTranscript(delta) {
  if (typeof delta !== "string" || delta.length === 0) return;
  inputTranscript += delta;
  setSubtitle("user", inputTranscript);
}

function setControlsMode(mode) {
  ui.controls.dataset.mode = mode;
  ui.start.hidden = mode !== "idle";
  ui.minimize.hidden = mode !== "active";
  ui.mute.hidden = mode !== "active";
  ui.end.hidden = mode === "idle";

  ui.start.disabled = mode !== "idle";
  ui.mute.disabled = mode !== "active" || !microphone;
  ui.end.disabled = mode === "ended";

  ui.startLabel.textContent = hasAttemptedSessionStart ? "Retry" : "Start";
  ui.endLabel.textContent = mode === "ended" ? "Ended" : "End";
}

function appendAssistantTranscript(delta) {
  if (typeof delta !== "string" || delta.length === 0) return;
  if (!assistantTranscriptStarted) {
    assistantTranscriptStarted = true;
    setSubtitle("assistant", "");
  }
  captionTranscript += delta;
  captionBuffer += delta;
  queueTranscriptCaptions();
}

function finishAssistantTranscript(item) {
  if (!item || item.type !== "message" || !Array.isArray(item.content)) return;
  const transcript = item.content
    .map((part) => typeof part.transcript === "string" ? part.transcript : "")
    .join("");
  if (transcript && transcript.startsWith(captionTranscript)) {
    appendAssistantTranscript(transcript.slice(captionTranscript.length));
  }
  queueTranscriptCaptions(true);
  if (assistantResponseHasAudio && !assistantOutputActive && captionQueue.length > 0) captionCatchUpActive = true;
  showNextAssistantCaption();
}

function showAssistantSubtitle() {
  if (captionQueue.length > 0) showNextAssistantCaption();
  if (ui.assistantTranscript.textContent.trim()) ui.assistantSubtitle.hidden = false;
}

function scheduleSubtitleHide() {
  if (subtitleHideTimer) clearTimeout(subtitleHideTimer);
  subtitleHideTimer = setTimeout(() => {
    subtitleHideTimer = null;
    if (!assistantOutputActive && !captionCatchUpActive && !captionTimer && captionQueue.length === 0) {
      setSubtitle("assistant", "");
    }
  }, 1800);
}

function clearSubtitles(preserveUserTranscript = false) {
  if (subtitleHideTimer) clearTimeout(subtitleHideTimer);
  if (captionTimer) clearTimeout(captionTimer);
  subtitleHideTimer = null;
  captionTimer = null;
  assistantOutputActive = false;
  captionCatchUpActive = false;
  assistantResponseHasAudio = false;
  assistantTranscriptStarted = false;
  captionTranscript = "";
  captionBuffer = "";
  captionQueue = [];
  setSubtitle("assistant", "");
  if (!preserveUserTranscript) {
    inputTranscript = "";
    setSubtitle("user", "");
  }
}

function setMuteButton() {
  ui.mute.dataset.muted = String(muted);
  ui.mute.setAttribute("aria-pressed", String(muted));
  ui.mute.setAttribute("aria-label", muted ? "Unmute microphone (M)" : "Mute microphone (M)");
  ui.muteLabel.textContent = muted ? "Unmute" : "Mute";
}

function toggleMute() {
  if (!microphone || ended) return false;
  muted = !muted;
  applyMicrophoneState();
  playToneSequence([{ frequency: 520, duration: 0.1 }], 0.035);
  return true;
}

async function playRemoteAudio() {
  if (!ui.audio.srcObject) return;
  ui.audio.autoplay = true;
  ui.audio.playsInline = true;
  ui.audio.muted = false;
  ui.audio.volume = 1;
  try {
    await ui.audio.play();
    ui.audioEnable.hidden = true;
  } catch (_) {
    ui.audioEnable.hidden = false;
  }
}

ui.audio.addEventListener("playing", () => {
  if (!ended) {
    assistantResponseHasAudio = true;
    assistantOutputActive = true;
    showAssistantSubtitle();
    setDuck("speaking");
  }
});
ui.audio.addEventListener("error", () => {
  if (!ended && remoteAudioTrack) ui.audioEnable.hidden = false;
});

function applyMicrophoneState() {
  if (microphone) {
    const enabled = !muted && !assistantAudioGateActive;
    for (const track of microphone.getAudioTracks()) track.enabled = enabled;
  }
  setMuteButton();
}

function gateMicrophoneForAssistantStart() {
  if (assistantAudioGateTimer) clearTimeout(assistantAudioGateTimer);
  assistantAudioGateTimer = null;
  if (!microphone || muted) {
    assistantAudioGateActive = false;
    applyMicrophoneState();
    return;
  }
  assistantAudioGateActive = true;
  applyMicrophoneState();
  assistantAudioGateTimer = setTimeout(() => {
    assistantAudioGateTimer = null;
    assistantAudioGateActive = false;
    applyMicrophoneState();
  }, 1200);
}

function releaseAssistantMicrophoneGate() {
  if (assistantAudioGateTimer) clearTimeout(assistantAudioGateTimer);
  assistantAudioGateTimer = null;
  assistantAudioGateActive = false;
  applyMicrophoneState();
}

function stopMedia() {
  clearSubtitles();
  if (assistantAudioGateTimer) clearTimeout(assistantAudioGateTimer);
  assistantAudioGateTimer = null;
  assistantAudioGateActive = false;
  if (channel) {
    channel.onmessage = null;
    channel.onopen = null;
    channel.onclose = null;
    channel.onerror = null;
    try { channel.close(); } catch (_) { /* already closed */ }
    channel = null;
  }
  if (peer) {
    peer.ontrack = null;
    peer.onconnectionstatechange = null;
    try { peer.close(); } catch (_) { /* already closed */ }
    peer = null;
  }
  if (remoteAudioTrack) {
    remoteAudioTrack.onmute = null;
    remoteAudioTrack.onunmute = null;
    remoteAudioTrack.onended = null;
    remoteAudioTrack = null;
  }
  if (microphone) {
    for (const track of microphone.getTracks()) track.stop();
    microphone = null;
  }
  ui.audio.pause();
  ui.audio.srcObject = null;
  ui.audioEnable.hidden = true;
  muted = false;
  setMuteButton();
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${token}`);
  const response = await fetch(path, {
    ...options,
    headers,
    cache: "no-store",
    credentials: "omit",
  });
  if (!response.ok) {
    const message = await response.text();
    const error = new Error(message.trim() || `Local request failed (${response.status}).`);
    error.name = "VoiceRequestError";
    throw error;
  }
  return response;
}

async function minimizeVoiceWindow() {
  try {
    await request("/api/minimize", { method: "POST" });
  } catch (error) {
    showError(error.message || "Could not minimize the voice window.");
  }
}

function isCurrentAttempt(attempt) {
  return !ended && attempt === sessionAttempt;
}

function handleVoiceEvent(raw) {
  let event;
  try {
    if (typeof raw !== "string") return;
    event = JSON.parse(raw);
  } catch (_) {
    return;
  }
  if (!event || typeof event.type !== "string") return;

  switch (event.type) {
    case "input_audio_buffer.speech_started":
      inputTranscript = "";
      clearSubtitles();
      setDuck("user-speaking");
      break;
    case "input_audio_buffer.speech_stopped":
      setDuck("thinking");
      break;
    case "response.created":
      clearSubtitles(Boolean(ui.userTranscript.textContent.trim()));
      captionTranscript = "";
      setDuck("thinking");
      break;
    case "conversation.item.input_audio_transcription.completed":
      if (typeof event.transcript === "string") showUserTranscript(event.transcript);
      break;
    case "conversation.item.input_audio_transcription.delta":
      appendUserTranscript(event.delta);
      break;
    case "response.output_audio_transcript.delta":
      appendAssistantTranscript(event.delta);
      break;
    case "response.output_item.done":
      finishAssistantTranscript(event.item);
      break;
    case "output_audio_buffer.started":
      assistantResponseHasAudio = true;
      captionCatchUpActive = false;
      assistantOutputActive = true;
      showAssistantSubtitle();
      setDuck("speaking");
      gateMicrophoneForAssistantStart();
      break;
    case "output_audio_buffer.stopped":
      assistantOutputActive = false;
      releaseAssistantMicrophoneGate();
      queueTranscriptCaptions(true);
      captionCatchUpActive = captionQueue.length > 0 || Boolean(captionTimer);
      if (captionCatchUpActive) showNextAssistantCaption();
      else scheduleSubtitleHide();
      if (!ended) setDuck("listening");
      break;
    case "output_audio_buffer.cleared":
      clearSubtitles();
      releaseAssistantMicrophoneGate();
      if (!ended) setDuck("listening");
      break;
    case "response.done":
      if (event.response && event.response.status === "failed") {
        clearSubtitles();
        releaseAssistantMicrophoneGate();
        showError("Duck.ai could not respond. Please try again.");
        setDuck("listening");
      } else {
        queueTranscriptCaptions(true);
        if (!assistantOutputActive && captionQueue.length > 0) captionCatchUpActive = true;
        showNextAssistantCaption();
        if (!assistantOutputActive && captionQueue.length === 0 && !captionTimer) scheduleSubtitleHide();
        if (!assistantOutputActive && !ended) setDuck("listening");
      }
      break;
    case "conversation.item.added":
      if (event.item && event.item.type === "function_call" && event.item.name === "session_terminated") {
        handleSessionTermination();
      }
      break;
    default:
      // Transcript and protocol detail stay out of the companion window.
      break;
  }
}

async function startSession() {
  if (!token) {
    showError("The local voice session is unavailable. Relaunch /speak from the terminal.");
    setConnection("error", "Session unavailable");
    ui.start.disabled = true;
    return;
  }
  if (ended) return;

  hasAttemptedSessionStart = true;
  const attempt = ++sessionAttempt;
  setControlsMode("starting");
  showError("");
  ui.audioEnable.hidden = true;
  setConnection("connecting", "Connecting…");
  setDuck("connecting");

  try {
    const missingFeatures = [];
    if (!window.isSecureContext) missingFeatures.push("secure context");
    if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== "function") missingFeatures.push("microphone capture");
    if (typeof RTCPeerConnection !== "function") missingFeatures.push("RTCPeerConnection");
    if (missingFeatures.length > 0) {
      const hint = " This conversation requires a recent version of Google Chrome or Chromium. Update your browser and relaunch /speak.";
      const error = new Error(`Missing features: ${missingFeatures.join(", ")}.${hint}`);
      error.name = "VoiceUIError";
      throw error;
    }

    const microphoneStream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: false, channelCount: 1, sampleRate: 48000 },
    });
    if (!isCurrentAttempt(attempt)) {
      for (const track of microphoneStream.getTracks()) track.stop();
      return;
    }
    microphone = microphoneStream;
    muted = false;
    applyMicrophoneState();
    setControlsMode("active");

    const iceResponse = await request("/api/ice-servers");
    if (!isCurrentAttempt(attempt)) return;
    const ice = await iceResponse.json();
    if (!isCurrentAttempt(attempt)) return;

    peer = new RTCPeerConnection({ iceServers: ice.iceServers, iceTransportPolicy: "relay" });
    for (const track of microphone.getAudioTracks()) peer.addTrack(track, microphone);
    peer.onconnectionstatechange = () => {
      if (!peer || ended) return;
      if (peer.connectionState === "connected") {
        if (!markVoiceConnectionReady()) setConnection("connecting", "Connecting…");
        if (channel?.readyState !== "open") setDuck("connecting");
      } else if (peer.connectionState === "connecting") {
        setConnection("connecting", "Connecting…");
        setDuck("connecting");
      } else if (peer.connectionState === "failed") {
        clearSubtitles();
        setConnection("error", "Connection interrupted");
        setDuck("error");
        showError("The audio connection failed. End the call and try again.");
      } else if (peer.connectionState === "disconnected") {
        setConnection("connecting", "Reconnecting…");
        setDuck("connecting");
      }
    };
    peer.ontrack = (event) => {
      if (ended) return;
      remoteAudioTrack = event.track;
      if (event.streams && event.streams[0]) {
        ui.audio.srcObject = event.streams[0];
      } else {
        const stream = ui.audio.srcObject || new MediaStream();
        stream.addTrack(event.track);
        ui.audio.srcObject = stream;
      }
      remoteAudioTrack.onunmute = () => {
        if (!ended) void playRemoteAudio();
      };
      remoteAudioTrack.onmute = () => {
        if (!ended) setDuck("listening");
      };
      remoteAudioTrack.onended = () => {
        if (!ended) setDuck("listening");
      };
      void playRemoteAudio();
    };

    channel = peer.createDataChannel("duckai-voice-session");
    channel.onmessage = (event) => handleVoiceEvent(event.data);
    channel.onopen = () => {
      if (ended) return;
      if (!markVoiceConnectionReady()) {
        setConnection("connecting", "Connecting…");
        setDuck("connecting");
      }
    };
    channel.onclose = () => {
      if (ended) return;
      clearSubtitles();
      setConnection("error", "Connection closed");
      setDuck("error");
      showError("The voice connection closed. End the call and try again.");
    };

    const offer = await peer.createOffer();
    if (!isCurrentAttempt(attempt)) return;
    await peer.setLocalDescription(offer);
    if (!isCurrentAttempt(attempt)) return;
    const answerResponse = await request("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/sdp" },
      body: peer.localDescription.sdp,
    });
    if (!isCurrentAttempt(attempt)) return;
    const answer = await answerResponse.text();
    if (!isCurrentAttempt(attempt)) return;
    await peer.setRemoteDescription({ type: "answer", sdp: answer });
  } catch (error) {
    stopMedia();
    if (!isCurrentAttempt(attempt)) return;

    setControlsMode("idle");
    setConnection("error", "Not connected");
    setDuck("error");
    if (error && error.name === "NotAllowedError") {
      showError("Microphone access was denied. Allow microphone access and try again.");
    } else if (error && error.name === "NotFoundError") {
      showError("No microphone detected. Connect one and try again.");
    } else if (error && error.name === "NotReadableError") {
      showError("The microphone is unavailable or already in use. Close other apps using it and try again.");
    } else if (error && error.name === "SecurityError") {
      showError("Microphone access is blocked. Check your browser permissions and try again.");
    } else if (error && (error.name === "VoiceRequestError" || error.name === "VoiceUIError")) {
      showError(error.message);
    } else {
      showError("Could not start the voice conversation. Check your connection and microphone, then try again.");
    }
  }
}

async function endSession(sendStop = true) {
  if (!ended) {
    ended = true;
    sessionAttempt += 1;
    clearSubtitles();
    stopMedia();
    setConnection("idle", "Ended");
    setDuck("ended");
    setControlsMode("ended");
    showError("");
  }
  if (sendStop && token) {
    try { await request("/api/stop", { method: "POST" }); } catch (_) { /* the CLI may already be stopping */ }
  }
}

function retrySession() {
  if (!ended) return;
  ended = false;
  ui.terminationPanel.hidden = true;
  ui.controls.hidden = false;
  ui.subtitleSlot.hidden = false;
  setControlsMode("idle");
  void startSession();
}

ui.start.addEventListener("click", () => void startSession());
ui.minimize.addEventListener("click", () => void minimizeVoiceWindow());
ui.mute.addEventListener("click", toggleMute);
ui.end.addEventListener("click", endActiveSession);
ui.retry.addEventListener("click", retrySession);
ui.closeVoice.addEventListener("click", () => void endSession(true));
ui.audioEnable.addEventListener("click", () => void playRemoteAudio());
window.addEventListener("keydown", (event) => {
  if (event.key.toLowerCase() !== "m" || event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
  const target = event.target;
  if (target instanceof HTMLElement && target.isContentEditable) return;
  if (target instanceof HTMLElement && target.closest("input, textarea, select, [contenteditable='true']")) return;
  if (toggleMute()) event.preventDefault();
});
window.addEventListener("pagehide", () => {
  sessionAttempt += 1;
  ended = true;
  stopMedia();
  if (token) {
    fetch("/api/stop", {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      keepalive: true,
      cache: "no-store",
      credentials: "omit",
    }).catch(() => {});
  }
});

setControlsMode("idle");
setMuteButton();
if (!token) {
  showError("The local voice session is unavailable. Relaunch /speak from the terminal.");
  setConnection("error", "Session unavailable");
  setDuck("error");
  ui.start.disabled = true;
  ui.start.hidden = true;
} else {
  void startSession();
}
