export function contextSeedEvents(messages, idPrefix) {
  if (!Array.isArray(messages)) return [];
  return messages.flatMap((message, index) => {
    if (!message || (message.role !== "user" && message.role !== "assistant")) return [];
    if (typeof message.content !== "string" || !message.content.trim()) return [];
    return [{
      type: "conversation.item.create",
      item: {
        id: `${idPrefix}-${index}`,
        type: "message",
        role: message.role,
        content: [{ type: message.role === "user" ? "input_text" : "output_text", text: message.content }],
      },
    }];
  });
}

export function canTransmitMicrophone(muted, assistantAudioGateActive, contextReady) {
  return !muted && !assistantAudioGateActive && contextReady;
}

export function completedTranscript(event) {
  if (!event || typeof event !== "object") return null;
  if (event.type === "conversation.item.input_audio_transcription.completed") {
    if (typeof event.item_id !== "string" || typeof event.transcript !== "string" || !event.transcript.trim()) return null;
    return { id: event.item_id, role: "user", content: event.transcript };
  }
  if (event.type !== "response.output_item.done" || !event.item || typeof event.item.id !== "string" || event.item.type !== "message" || !Array.isArray(event.item.content)) {
    return null;
  }
  if (event.item.status !== "completed") return null;
  const content = event.item.content
    .map((part) => typeof part.transcript === "string" ? part.transcript : typeof part.text === "string" ? part.text : "")
    .join("");
  if (!content.trim()) return null;
  return { id: event.item.id, role: "assistant", content };
}

export function boundVoiceContext(messages) {
  if (!Array.isArray(messages)) return [];
  const selected = [];
  const maxMessages = 24;
  let remainingBytes = 48 * 1024;
  for (let index = messages.length - 1; index >= 0 && selected.length < maxMessages && remainingBytes > 0; index -= 1) {
    const message = messages[index];
    if (!message || (message.role !== "user" && message.role !== "assistant")) continue;
    if (typeof message.content !== "string" || !message.content.trim()) continue;
    const content = truncateUTF8Tail(message.content, Math.min(8 * 1024, remainingBytes));
    if (!content) continue;
    remainingBytes -= new TextEncoder().encode(content).byteLength;
    selected.push({ role: message.role, content });
  }
  return selected.reverse();
}

function truncateUTF8Tail(value, maxBytes) {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const bytes = encoder.encode(value);
  if (bytes.byteLength <= maxBytes) return value;
  let start = bytes.byteLength - maxBytes;
  while (start < bytes.byteLength && (bytes[start] & 0xc0) === 0x80) start += 1;
  return decoder.decode(bytes.subarray(start));
}

export class OrderedTranscriptQueue {
  constructor() {
    this.pending = [];
    this.blocked = false;
  }

  enqueue(messages) {
    if (Array.isArray(messages)) this.pending.push(...messages);
  }

  next() {
    return this.blocked ? null : this.pending[0] || null;
  }

  acknowledge(itemID) {
    if (!this.pending.length || this.pending[0].id !== itemID) return false;
    this.pending.shift();
    return true;
  }

  fail() {
    this.blocked = true;
  }

  retry() {
    this.blocked = false;
  }

  get size() {
    return this.pending.length;
  }
}

export function transcriptDeliveryPayload(message, maxContentBytes = 64 * 1024) {
  if (new TextEncoder().encode(message.content).byteLength <= maxContentBytes) return message;
  return { id: message.id, role: message.role, omitted: true };
}

export function isPermanentTranscriptFailure(status) {
  return status === 400 || status === 413;
}

export class TranscriptCollector {
  constructor() {
    this.turns = [];
    this.userTurns = new Map();
    this.responseTurns = new Map();
    this.ignoredItemIDs = new Set();
    this.seenTranscriptIDs = new Set();
    this.activeTurn = null;
    this.postedTurnCount = 0;
  }

  ignoreItemIDs(itemIDs) {
    for (const itemID of itemIDs) this.ignoredItemIDs.add(itemID);
  }

  add(event) {
    if (!event || typeof event !== "object") return;
    if (event.type === "input_audio_buffer.committed" && typeof event.item_id === "string") {
      this.ensureUserTurn(event.item_id);
      return;
    }
    if (event.type === "conversation.item.created" && event.item?.role === "user" && typeof event.item.id === "string") {
      if (!this.ignoredItemIDs.has(event.item.id)) this.ensureUserTurn(event.item.id);
      return;
    }
    if (event.type === "conversation.item.input_audio_transcription.completed") {
      if (this.ignoredItemIDs.has(event.item_id)) return;
      const message = completedTranscript(event);
      if (!message || this.seenTranscriptIDs.has(message.id)) return;
      const turn = this.ensureUserTurn(message.id);
      turn.userMessage = message;
      turn.userFinalized = true;
      this.seenTranscriptIDs.add(message.id);
      return;
    }
    if (event.type === "conversation.item.input_audio_transcription.failed") {
      if (!this.ignoredItemIDs.has(event.item_id)) this.ensureUserTurn(event.item_id).userFinalized = true;
      return;
    }
    if (event.type === "response.created") {
      const responseID = event.response?.id;
      if (typeof responseID !== "string") return;
      const turn = this.activeTurn || this.newTurn();
      this.responseTurns.set(responseID, turn);
      return;
    }
    if (event.type === "response.done") {
      const responseID = event.response?.id;
      const turn = this.responseTurns.get(responseID);
      if (!turn) return;
      turn.responseDone = true;
      turn.responseStatus = event.response.status;
      return;
    }
    if (event.type === "response.output_item.done") {
      const message = completedTranscript(event);
      if (!message || this.seenTranscriptIDs.has(message.id)) return;
      const turn = this.responseTurns.get(event.response_id) || this.activeTurn || this.newTurn();
      turn.assistantMessages.push({
        message,
        outputIndex: Number.isFinite(event.output_index) ? event.output_index : turn.assistantMessages.length,
        sequence: turn.assistantMessages.length,
      });
      turn.assistantMessages.sort((left, right) => left.outputIndex - right.outputIndex || left.sequence - right.sequence);
      this.seenTranscriptIDs.add(message.id);
    }
  }

  messages() {
    return this.turns.flatMap((turn) => [
      ...(turn.userMessage ? [turn.userMessage] : []),
      ...(turn.responseStatus === "completed" ? turn.assistantMessages.map(({ message }) => message) : []),
    ]);
  }

  takeReadyMessages(force = false) {
    const messages = [];
    while (this.postedTurnCount < this.turns.length) {
      const turn = this.turns[this.postedTurnCount];
      if (!force && (!turn.userFinalized || !turn.responseDone)) break;
      if (turn.userMessage) messages.push(turn.userMessage);
      if (turn.responseDone && turn.responseStatus === "completed") {
        messages.push(...turn.assistantMessages.map(({ message }) => message));
      }
      this.postedTurnCount += 1;
    }
    return messages;
  }

  ensureUserTurn(itemID) {
    if (typeof itemID === "string" && this.userTurns.has(itemID)) {
      return this.userTurns.get(itemID);
    }
    const turn = this.newTurn();
    if (typeof itemID === "string") this.userTurns.set(itemID, turn);
    turn.userItemID = itemID;
    turn.userFinalized = false;
    this.activeTurn = turn;
    return turn;
  }

  newTurn() {
    const turn = { userMessage: null, userFinalized: true, responseDone: false, responseStatus: "", assistantMessages: [] };
    this.turns.push(turn);
    this.activeTurn = turn;
    return turn;
  }
}
