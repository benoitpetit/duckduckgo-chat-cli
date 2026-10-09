// Package activity provides a bounded, in-memory feed of safe CLI activity.
package activity

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MaxEvents            = 200
	MaxConversationRunes = 4000
	subscriberQueue      = 64
)

var (
	localPathPattern = regexp.MustCompile("(?m)(^|[\\s(\"'=])((?:[A-Za-z]:\\\\|/)[^\\s<>|\"']+)")
	secretPattern    = regexp.MustCompile(`(?im)(\b(?:api[_ -]?key|token|password|secret|authorization|cookie)\s*[:=]\s*)[^\r\n]+`)
)

// Event is a structured activity record. Conversation fields are populated
// only while the user has explicitly enabled conversation content.
type Event struct {
	ID          uint64    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Category    string    `json:"category"`
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	Model       string    `json:"model,omitempty"`
	OperationID string    `json:"operationId,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	Response    string    `json:"response,omitempty"`
}

type subscriber struct {
	ids chan uint64
}

// Hub stores recent activity and broadcasts event IDs to local consumers.
// Subscribers resolve IDs back through the hub, so disabling privacy can
// scrub queued-but-not-yet-written conversation fields as well.
type Hub struct {
	mu             sync.Mutex
	events         []Event
	nextID         uint64
	contentEnabled bool
	active         map[string]string
	subscribers    map[uint64]subscriber
	nextSubscriber uint64
}

func NewHub() *Hub {
	return &Hub{active: make(map[string]string), subscribers: make(map[uint64]subscriber)}
}

// Publish appends an event and returns the stored, privacy-filtered event.
func (h *Hub) Publish(event Event) Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	event.ID = h.nextID
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	if event.Status == "started" && event.OperationID == "" {
		event.OperationID = strconv.FormatUint(event.ID, 10)
	}
	if !h.contentEnabled {
		event.Prompt, event.Response = "", ""
		if event.Category == "conversation" {
			event.Summary = hiddenConversationSummary(event.Status)
		}
	} else {
		event.Prompt = truncateRunes(sanitizeConversation(event.Prompt), MaxConversationRunes)
		event.Response = truncateRunes(sanitizeConversation(event.Response), MaxConversationRunes)
	}
	h.events = append(h.events, event)
	if len(h.events) > MaxEvents {
		copy(h.events, h.events[len(h.events)-MaxEvents:])
		h.events = h.events[:MaxEvents]
	}
	h.updateCurrent(event)
	for id, sub := range h.subscribers {
		select {
		case sub.ids <- event.ID:
		default:
			close(sub.ids)
			delete(h.subscribers, id)
		}
	}
	return event
}

func (h *Hub) updateCurrent(event Event) {
	if event.OperationID == "" {
		return
	}
	switch event.Status {
	case "started":
		h.active[event.OperationID] = event.Summary
	case "completed", "failed", "cancelled":
		delete(h.active, event.OperationID)
	}
}

// SetConversationContentEnabled applies the privacy preference immediately.
// Pending subscriber queues contain IDs only, so subsequent reads see scrubbed
// records when content is turned off.
func (h *Hub) SetConversationContentEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.contentEnabled = enabled
	if enabled {
		return
	}
	for i := range h.events {
		h.events[i].Prompt = ""
		h.events[i].Response = ""
		if h.events[i].Category == "conversation" {
			h.events[i].Summary = hiddenConversationSummary(h.events[i].Status)
		}
	}
}

func hiddenConversationSummary(status string) string {
	switch status {
	case "prompt":
		return "User prompt submitted (hidden)"
	case "response":
		return "Assistant response received (hidden)"
	default:
		return "Conversation content hidden"
	}
}

// Subscribe atomically returns retained events after afterID and attaches a
// subscriber. If the cursor predates retained history, it returns a fresh
// bounded snapshot.
func (h *Hub) Subscribe(afterID uint64) (replay []Event, ids <-chan uint64, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	replay = h.eventsAfterLocked(afterID)
	h.nextSubscriber++
	subscriberID := h.nextSubscriber
	queue := make(chan uint64, subscriberQueue)
	h.subscribers[subscriberID] = subscriber{ids: queue}
	return replay, queue, func() {
		h.mu.Lock()
		if sub, ok := h.subscribers[subscriberID]; ok {
			delete(h.subscribers, subscriberID)
			close(sub.ids)
		}
		h.mu.Unlock()
	}
}

// EventsAfter returns current retained events after a cursor. A cursor that
// fell behind retention receives the complete current snapshot.
func (h *Hub) EventsAfter(afterID uint64) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.eventsAfterLocked(afterID)
}

func (h *Hub) eventsAfterLocked(afterID uint64) []Event {
	if len(h.events) == 0 {
		return nil
	}
	if afterID > h.nextID {
		afterID = 0
	}
	if afterID+1 < h.events[0].ID {
		afterID = 0
	}
	result := make([]Event, 0, len(h.events))
	for _, event := range h.events {
		if event.ID > afterID {
			result = append(result, event)
		}
	}
	return result
}

// Snapshot returns a copy of recent events and the current active operation.
func (h *Hub) Snapshot() (events []Event, current string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	events = append([]Event(nil), h.events...)
	if len(h.active) != 0 {
		// The most recently started operation is the one with the largest ID.
		var latest uint64
		for id, summary := range h.active {
			n, _ := strconv.ParseUint(id, 10, 64)
			if n >= latest {
				latest, current = n, summary
			}
		}
	}
	return events, current
}

func truncateRunes(value string, limit int) string {
	if utf8RuneCount(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "… [truncated]"
}

func sanitizeConversation(value string) string {
	value = localPathPattern.ReplaceAllString(value, "$1[local path]")
	return secretPattern.ReplaceAllString(value, "$1[redacted]")
}

func utf8RuneCount(value string) int { return len([]rune(value)) }
