import assert from "node:assert/strict";
import test from "node:test";

import { boundVoiceContext, contextSeedEvents, completedTranscript, canTransmitMicrophone, isPermanentTranscriptFailure, OrderedTranscriptQueue, transcriptDeliveryPayload, TranscriptCollector } from "./context.mjs";

test("seeds user and assistant messages in the same order", () => {
  assert.deepEqual(contextSeedEvents([
    { role: "user", content: "What is the result?" },
    { role: "assistant", content: "The result is 42." },
    { role: "system", content: "Do not include this." },
  ], "seed"), [
    {
      type: "conversation.item.create",
      item: { id: "seed-0", type: "message", role: "user", content: [{ type: "input_text", text: "What is the result?" }] },
    },
    {
      type: "conversation.item.create",
      item: { id: "seed-1", type: "message", role: "assistant", content: [{ type: "output_text", text: "The result is 42." }] },
    },
  ]);
});

test("extracts completed user and assistant transcripts", () => {
  assert.deepEqual(completedTranscript({
    type: "conversation.item.input_audio_transcription.completed",
    item_id: "user-1",
    transcript: "Keep this context.",
  }), { id: "user-1", role: "user", content: "Keep this context." });
  assert.deepEqual(completedTranscript({
    type: "response.output_item.done",
    item: { id: "assistant-1", type: "message", status: "completed", content: [{ transcript: "I will." }] },
  }), { id: "assistant-1", role: "assistant", content: "I will." });
});

test("ignores interrupted assistant items", () => {
  assert.equal(completedTranscript({
    type: "response.output_item.done",
    item: { id: "assistant-cut-off", type: "message", status: "incomplete", content: [{ transcript: "half a reply" }] },
  }), null);
  assert.equal(completedTranscript({
    type: "response.output_item.done",
    item: { id: "assistant-unknown", type: "message", content: [{ transcript: "status missing" }] },
  }), null);
});

test("bounds retry context to recent user and assistant text", () => {
  const messages = Array.from({ length: 26 }, (_, index) => ({ role: "user", content: `turn-${index}` }));
  messages.unshift({ role: "system", content: "ignore" });
  const bounded = boundVoiceContext(messages);
  assert.equal(bounded.length, 24);
  assert.equal(bounded[0].content, "turn-2");
  assert.equal(bounded.at(-1).content, "turn-25");
});

test("keeps transcript turns in conversation order when user transcription arrives late", () => {
  const collector = new TranscriptCollector();
  collector.add({ type: "input_audio_buffer.committed", item_id: "user-1" });
  collector.add({ type: "response.created", response: { id: "response-1" } });
  collector.add({
    type: "response.output_item.done",
    response_id: "response-1",
    output_index: 0,
    item: { id: "assistant-1", type: "message", status: "completed", content: [{ transcript: "First reply." }] },
  });
  collector.add({ type: "response.done", response: { id: "response-1", status: "completed" } });
  collector.add({ type: "conversation.item.input_audio_transcription.completed", item_id: "user-1", transcript: "First question." });
  collector.add({ type: "input_audio_buffer.committed", item_id: "user-2" });
  collector.add({ type: "conversation.item.input_audio_transcription.completed", item_id: "user-2", transcript: "Second question." });
  collector.add({ type: "response.created", response: { id: "response-2" } });
  collector.add({
    type: "response.output_item.done",
    response_id: "response-2",
    output_index: 0,
    item: { id: "assistant-2", type: "message", status: "completed", content: [{ transcript: "Second reply." }] },
  });
  collector.add({ type: "response.done", response: { id: "response-2", status: "completed" } });

  assert.deepEqual(collector.messages(), [
    { id: "user-1", role: "user", content: "First question." },
    { id: "assistant-1", role: "assistant", content: "First reply." },
    { id: "user-2", role: "user", content: "Second question." },
    { id: "assistant-2", role: "assistant", content: "Second reply." },
  ]);
});

test("late transcription does not rewind the current response turn", () => {
  const collector = new TranscriptCollector();
  collector.add({ type: "input_audio_buffer.committed", item_id: "user-1" });
  collector.add({ type: "response.created", response: { id: "response-1" } });
  collector.add({
    type: "response.output_item.done",
    response_id: "response-1",
    output_index: 0,
    item: { id: "assistant-1", type: "message", status: "completed", content: [{ transcript: "First reply." }] },
  });
  collector.add({ type: "response.done", response: { id: "response-1", status: "completed" } });
  collector.add({ type: "input_audio_buffer.committed", item_id: "user-2" });
  collector.add({ type: "conversation.item.input_audio_transcription.completed", item_id: "user-1", transcript: "First question." });
  collector.add({ type: "response.created", response: { id: "response-2" } });
  collector.add({ type: "conversation.item.input_audio_transcription.completed", item_id: "user-2", transcript: "Second question." });
  collector.add({
    type: "response.output_item.done",
    response_id: "response-2",
    output_index: 0,
    item: { id: "assistant-2", type: "message", status: "completed", content: [{ transcript: "Second reply." }] },
  });
  collector.add({ type: "response.done", response: { id: "response-2", status: "completed" } });

  assert.deepEqual(collector.messages(), [
    { id: "user-1", role: "user", content: "First question." },
    { id: "assistant-1", role: "assistant", content: "First reply." },
    { id: "user-2", role: "user", content: "Second question." },
    { id: "assistant-2", role: "assistant", content: "Second reply." },
  ]);
});

test("releases a finalized turn only after asynchronous user transcription is ready", () => {
  const collector = new TranscriptCollector();
  collector.add({ type: "input_audio_buffer.committed", item_id: "user-1" });
  collector.add({ type: "response.created", response: { id: "response-1" } });
  collector.add({
    type: "response.output_item.done",
    response_id: "response-1",
    output_index: 0,
    item: { id: "assistant-1", type: "message", status: "completed", content: [{ transcript: "Answer." }] },
  });
  collector.add({ type: "response.done", response: { id: "response-1", status: "completed" } });
  assert.deepEqual(collector.takeReadyMessages(), []);

  collector.add({ type: "conversation.item.input_audio_transcription.completed", item_id: "user-1", transcript: "Question." });
  assert.deepEqual(collector.takeReadyMessages(), [
    { id: "user-1", role: "user", content: "Question." },
    { id: "assistant-1", role: "assistant", content: "Answer." },
  ]);
  assert.deepEqual(collector.takeReadyMessages(), []);
});

test("keeps later transcript items behind a failed FIFO head until retry succeeds", () => {
  const queue = new OrderedTranscriptQueue();
  const user = { id: "user-1", role: "user", content: "Question." };
  const assistant = { id: "assistant-1", role: "assistant", content: "Answer." };
  const later = { id: "user-2", role: "user", content: "Next question." };
  queue.enqueue([user, assistant]);
  assert.equal(queue.next(), user);
  assert.equal(queue.acknowledge(assistant.id), false);
  assert.equal(queue.next(), user);
  queue.fail();
  queue.enqueue([later]);
  assert.equal(queue.next(), null);

  queue.retry();
  assert.equal(queue.next(), user);
  assert.equal(queue.acknowledge(user.id), true);
  assert.equal(queue.next(), assistant);
  assert.equal(queue.acknowledge(assistant.id), true);
  assert.equal(queue.next(), later);
  assert.equal(queue.acknowledge(later.id), true);
  assert.equal(queue.next(), null);
});

test("converts oversized transcripts to a stable omission marker", () => {
  const oversized = { id: "long-1", role: "user", content: "x".repeat(64 * 1024 + 1) };
  assert.deepEqual(transcriptDeliveryPayload(oversized), { id: "long-1", role: "user", omitted: true });
  const bounded = { id: "small-1", role: "assistant", content: "x".repeat(64 * 1024) };
  assert.equal(transcriptDeliveryPayload(bounded), bounded);
});

test("recognizes only permanent transcript validation failures", () => {
  assert.equal(isPermanentTranscriptFailure(400), true);
  assert.equal(isPermanentTranscriptFailure(413), true);
  assert.equal(isPermanentTranscriptFailure(429), false);
  assert.equal(isPermanentTranscriptFailure(503), false);
});

test("does not add seeded history to the collected voice transcript", () => {
  const collector = new TranscriptCollector();
  collector.ignoreItemIDs(["seed-user"]);
  collector.add({
    type: "conversation.item.created",
    previous_item_id: null,
    item: { id: "seed-user", role: "user", type: "message" },
  });
  assert.deepEqual(collector.messages(), []);
});

test("keeps microphone transmission disabled until context is ready", () => {
  assert.equal(canTransmitMicrophone(false, false, false), false);
  assert.equal(canTransmitMicrophone(true, false, true), false);
  assert.equal(canTransmitMicrophone(false, true, true), false);
  assert.equal(canTransmitMicrophone(false, false, true), true);
});
