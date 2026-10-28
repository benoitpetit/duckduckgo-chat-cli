package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ptytest"

	"github.com/fatih/color"
)

type fixedCoordinateRecoveryPrompt struct {
	action CoordinateRecoveryAction
	calls  int
}

func (p *fixedCoordinateRecoveryPrompt) ChooseRecovery(*CoordinateRun) (CoordinateRecoveryAction, error) {
	p.calls++
	return p.action, nil
}

func newCoordinateTestChat(t *testing.T) (*Chat, *config.Config) {
	t.Helper()
	cfg := &config.Config{ExportDir: t.TempDir()}
	// Isolate coordinate saves from the user's real config file.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg.Dashboard.RetentionDays = 90
	chat := NewChat("", "", "", "", models.GPT6Luna, cfg)
	chat.coordinateArchivePrompt = &fixedCoordinateArchivePrompt{}
	return chat, cfg
}

func TestCoordinatedQuestionCommitsOnlyFinalAnswer(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Messages = []Message{{Role: "user", Content: "Earlier"}, {Role: "assistant", Content: "Context"}}
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	prior, err := c.durableStreamForRequest()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	runner := func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		calls++
		return []string{"private draft", "private check", "public final"}[calls-1], nil
	}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	run, err := c.RunCoordinatedQuestion(context.Background(), plan, "New question", cfg, runner, nil)
	if err != nil || run.FinalAnswer != "public final" || !run.Complete {
		t.Fatalf("RunCoordinatedQuestion() = %#v, %v", run, err)
	}
	if len(c.Messages) != 2 || len(c.pendingImages) != 1 {
		t.Fatalf("RunCoordinatedQuestion mutated principal transcript: %#v", c.Messages)
	}
	if err := c.CommitCoordinatedQuestion("New question", run); err != nil {
		t.Fatal(err)
	}
	if err := c.CommitCoordinatedQuestion("New question", run); err == nil {
		t.Fatal("committing the same coordinate result twice succeeded")
	}
	if len(c.Messages) != 4 || c.Messages[2].Content != "New question" || c.Messages[3].Content != "public final" {
		t.Fatalf("committed transcript = %#v", c.Messages)
	}
	if len(c.Messages[2].Images) != 1 || len(c.pendingImages) != 0 {
		t.Fatal("accepted coordinate run did not commit its pending reference image")
	}
	for _, message := range c.Messages {
		if strings.Contains(message.Content, "private draft") || strings.Contains(message.Content, "private check") {
			t.Fatal("private agent output entered principal history")
		}
	}
	next, err := c.durableStreamForRequest()
	if err != nil || next.ConversationID == prior.ConversationID {
		t.Fatalf("durable conversation identity was reused after coordination: %v", err)
	}
}

func TestCoordinatedQuestionDoesNotPersistIntermediateStageOutput(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Analytics = analytics.NewChatAnalytics()
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	calls := 0
	runner := func(_ context.Context, _ CoordinateAgent, prompt string) (string, error) {
		calls++
		if strings.Contains(prompt, "Write one clear final answer") {
			return "public final answer", nil
		}
		return []string{"private intermediate alpha", "private intermediate beta"}[calls-1], nil
	}
	run, err := c.RunCoordinatedQuestion(context.Background(), plan, "Question", cfg, runner, nil)
	if err != nil || run.FinalAnswer != "public final answer" {
		t.Fatalf("RunCoordinatedQuestion() = %#v, %v", run, err)
	}
	if len(c.Messages) != 0 {
		t.Fatalf("uncommitted run changed messages: %#v", c.Messages)
	}
	for name, value := range map[string]any{"config": cfg, "analytics": c.Analytics} {
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if strings.Contains(string(data), "private intermediate") {
			t.Fatalf("intermediate stage content was persisted in %s", name)
		}
	}
	if err := c.CommitCoordinatedQuestion("Question", run); err != nil {
		t.Fatal(err)
	}
	if len(c.Messages) != 2 || c.Messages[1].Content != "public final answer" {
		t.Fatalf("committed conversation = %#v", c.Messages)
	}
	for _, message := range c.Messages {
		if strings.Contains(message.Content, "private intermediate") {
			t.Fatal("intermediate stage content entered the main conversation")
		}
	}
}

func TestCoordinatedQuestionRateLimitPreservesHistoryAndAttachments(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Messages = []Message{{Role: "user", Content: "Earlier"}}
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	plan := coordinateTestPlan(CoordinateChain, 0)
	plan.Agents[2].Model = models.GPT54Mini
	calls := 0
	runner := func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		calls++
		return "", &RateLimitError{Code: "ERR_RATE_LIMIT"}
	}
	_, err := c.RunCoordinatedQuestion(context.Background(), plan, "New question", cfg, runner, nil)
	if !errors.Is(err, ErrRateLimited) || calls != 1 {
		t.Fatalf("error=%v, calls=%d; want persistent rate limit after first agent", err, calls)
	}
	if len(c.Messages) != 1 || c.Messages[0].Content != "Earlier" || len(c.pendingImages) != 1 {
		t.Fatal("failed coordination changed principal history or pending image")
	}
}

func TestCoordinatedQuestionRejectsUnsupportedImageModelBeforeCalls(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents[1].Model = models.GPTOSS120B
	calls := 0
	runner := func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		calls++
		return "answer", nil
	}
	_, err := c.RunCoordinatedQuestion(context.Background(), plan, "Question", cfg, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot receive images") || calls != 0 {
		t.Fatalf("error=%v, calls=%d; want image compatibility rejection", err, calls)
	}
	if len(c.pendingImages) != 1 {
		t.Fatal("incompatible image was discarded")
	}
}

func TestCoordinateNextQuestionConsumesPlanOnce(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.coordinateRunner = func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		calls++
		return []string{"draft A", "draft B", "final"}[calls-1], nil
	}
	ProcessInput(c, "One question", cfg)
	if c.HasNextCoordinate() || calls != 3 || len(c.Messages) != 2 || c.Messages[1].Content != "final" {
		t.Fatalf("one-shot coordinate state: armed=%v, calls=%d, messages=%#v", c.HasNextCoordinate(), calls, c.Messages)
	}
}

func TestCoordinateQuestionCanSaveReportAfterCommittingAnswer(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	prompt := &fixedCoordinateArchivePrompt{save: true}
	c.coordinateArchivePrompt = prompt
	c.coordinateRunner = func(_ context.Context, _ CoordinateAgent, stagePrompt string) (string, error) {
		if strings.Contains(stagePrompt, "Write one clear final answer") {
			return "public coordinate answer", nil
		}
		return "private intermediate response", nil
	}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	ProcessInput(c, "Question for the team", cfg)
	if prompt.calls != 1 || len(c.CoordinateReports) != 1 {
		t.Fatalf("archive prompt calls=%d reports=%+v", prompt.calls, c.CoordinateReports)
	}
	if len(c.Messages) != 2 || c.Messages[1].Content != "public coordinate answer" || strings.Contains(c.Messages[1].Content, "private intermediate") {
		t.Fatalf("committed chat messages = %+v", c.Messages)
	}
	saved, err := c.HistoryManager.LoadSession(c.SessionID)
	if err != nil {
		t.Fatalf("load saved coordinate report: %v", err)
	}
	if len(saved.CoordinateReports) != 1 || saved.CoordinateReports[0].Stages[0].Response != "private intermediate response" {
		t.Fatalf("saved coordinate reports = %+v", saved.CoordinateReports)
	}
}

func TestCoordinateStopLeavesHistoryUnchanged(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Messages = []Message{{Role: "user", Content: "Before"}, {Role: "assistant", Content: "Earlier answer"}}
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	c.coordinateRunner = func(_ context.Context, agent CoordinateAgent, _ string) (string, error) {
		if agent.Model == plan.Agents[0].Model {
			return "private draft", nil
		}
		return "", errors.New("second agent unavailable")
	}
	recovery := &fixedCoordinateRecoveryPrompt{action: CoordinateRecoveryStop}
	c.coordinateRecoveryPrompt = recovery
	ProcessInput(c, "New question", cfg)
	if recovery.calls != 1 {
		t.Fatalf("recovery menu calls = %d, want one", recovery.calls)
	}
	if len(c.Messages) != 2 || c.Messages[0].Content != "Before" || c.Messages[1].Content != "Earlier answer" {
		t.Fatalf("Stop changed the principal transcript: %#v", c.Messages)
	}
	if len(c.pendingImages) != 1 || c.pendingImages[0].Name != "reference.png" {
		t.Fatal("Stop discarded the pending image")
	}
}

func TestCoordinatePartialRecoveryCommitsOnlyThePartialAnswer(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	c.coordinateRunner = func(_ context.Context, agent CoordinateAgent, prompt string) (string, error) {
		if strings.Contains(prompt, "Write a partial answer") {
			return "partial final", nil
		}
		calls[agent.Role]++
		if agent.Model == plan.Agents[0].Model {
			return "available draft", nil
		}
		return "", errors.New("second agent unavailable")
	}
	c.coordinateRecoveryPrompt = &fixedCoordinateRecoveryPrompt{action: CoordinateRecoverySynthesizePartial}
	ProcessInput(c, "Question", cfg)
	if len(c.Messages) != 2 || !strings.Contains(c.Messages[1].Content, "partial final") || !strings.Contains(c.Messages[1].Content, "Partial coordinate result") {
		t.Fatalf("partial recovery transcript = %#v", c.Messages)
	}
	if strings.Contains(c.Messages[1].Content, "available draft") || calls[plan.Agents[0].Role] != 1 {
		t.Fatal("partial recovery replayed a completed stage or exposed its private draft")
	}
}

func TestCoordinateCancelStopsParallelRecoveryWithoutCommittingHistory(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Messages = []Message{{Role: "user", Content: "Before"}}
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	plan.Parallel = true
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	c.coordinateRunner = func(ctx context.Context, _ CoordinateAgent, _ string) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}
	recovery := &fixedCoordinateRecoveryPrompt{action: CoordinateRecoveryStop}
	c.coordinateRecoveryPrompt = recovery
	done := make(chan struct{})
	go func() {
		ProcessInput(c, "Cancelled question", cfg)
		close(done)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			c.CancelCurrentRequest()
			<-done
			t.Fatal("both parallel agents did not start")
		}
	}
	if !c.CancelCurrentRequest() {
		t.Fatal("active coordinate request did not expose cancellation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled coordinate run did not stop")
	}
	if recovery.calls != 0 {
		t.Fatalf("recovery menu calls = %d after cancellation, want zero", recovery.calls)
	}
	if len(c.Messages) != 1 || c.Messages[0].Content != "Before" {
		t.Fatalf("cancellation changed the principal transcript: %#v", c.Messages)
	}
	if len(c.pendingImages) != 1 || c.pendingImages[0].Name != "reference.png" {
		t.Fatal("cancellation discarded the pending image")
	}
}

func TestCoordinateIncompatibleImageKeepsNextQuestionArmed(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	if err := c.SetNextCoordinate(plan); err != nil {
		t.Fatal(err)
	}
	c.pendingImages = []media.ImageAttachment{{Name: "reference.png", Data: []byte("image")}}
	ProcessInput(c, "One question", cfg)
	if !c.HasNextCoordinate() || len(c.Messages) != 0 || len(c.pendingImages) != 1 {
		t.Fatal("incompatible image consumed the coordinate plan or attachment")
	}
}

func TestCoordinateEndNoticeIsRetainedAboveTheNextPrompt(t *testing.T) {
	c, _ := newCoordinateTestChat(t)
	c.Display = NewConversationView("DuckDuckGo AI Chat CLI", "", false)
	announceCoordinateEnded(c)
	c.Display.mu.Lock()
	defer c.Display.mu.Unlock()
	if len(c.Display.entries) != 1 || c.Display.entries[0].panelTitle != "Coordinate complete" || !strings.Contains(c.Display.entries[0].panelLines[0][0].Text, "normal chat") {
		t.Fatalf("coordinate completion notice was not retained in the conversation view: %#v", c.Display.entries)
	}
}

func TestCoordinateWorkflowRetainsFinalProgressPanel(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Display = NewConversationView("DuckDuckGo AI Chat CLI", "", false)
	c.coordinateRunner = func(_ context.Context, agent CoordinateAgent, _ string) (string, error) {
		return string(agent.Model) + " response", nil
	}
	if err := c.SetNextCoordinate(coordinateTestPlan(CoordinateIndependent, 0)); err != nil {
		t.Fatal(err)
	}
	ProcessInput(c, "Question", cfg)

	c.Display.mu.Lock()
	defer c.Display.mu.Unlock()
	var progress *displayEntry
	count := 0
	for i := range c.Display.entries {
		if c.Display.entries[i].panelTitle == "Coordinate progress" {
			progress = &c.Display.entries[i]
			count++
		}
	}
	if count != 1 || progress == nil {
		t.Fatalf("retained progress panels = %d, want one updated panel", count)
	}
	if len(progress.panelLines) != 3 {
		t.Fatalf("progress panel lines = %d, want three planned stages", len(progress.panelLines))
	}
	for _, line := range progress.panelLines {
		if len(line) < 3 || line[2].Text != " · completed" {
			t.Errorf("final progress line = %#v, want completed stage", line)
		}
	}
}

func TestCoordinateProvenancePanelFollowsAnswerBeforeEndNotice(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Display = NewConversationView("DuckDuckGo AI Chat CLI", "", false)
	c.coordinateRunner = func(_ context.Context, _ CoordinateAgent, prompt string) (string, error) {
		if strings.Contains(prompt, "Write one clear final answer") {
			return "final response", nil
		}
		return "agent response", nil
	}
	if err := c.SetNextCoordinate(coordinateTestPlan(CoordinateIndependent, 0)); err != nil {
		t.Fatal(err)
	}
	ProcessInput(c, "Question", cfg)

	c.Display.mu.Lock()
	defer c.Display.mu.Unlock()
	answerIndex, provenanceIndex, endIndex := -1, -1, -1
	for index, entry := range c.Display.entries {
		if entry.model == "Coordinate" {
			answerIndex = index
		}
		if entry.panelTitle == "Coordinate results" {
			provenanceIndex = index
		}
		if entry.panelTitle == "Coordinate complete" {
			endIndex = index
		}
	}
	if answerIndex < 0 || provenanceIndex <= answerIndex || endIndex <= provenanceIndex {
		t.Fatalf("answer/provenance/end notice indexes = %d/%d/%d; want that order", answerIndex, provenanceIndex, endIndex)
	}
}

func TestCoordinateAnswerKeepsItsFrameWhenGlobalFramesAreDisabled(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("pty unsupported: %v", err)
	}
	defer master.Close()
	if err := ptytest.SetSize(slave, 80, 24); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	previousStdout, previousColorOutput, previousNoColor := os.Stdout, color.Output, color.NoColor
	os.Stdout = slave
	color.Output = slave
	color.NoColor = false
	defer func() {
		os.Stdout, color.Output, color.NoColor = previousStdout, previousColorOutput, previousNoColor
	}()

	c, cfg := newCoordinateTestChat(t)
	cfg.Appearance.FrameResponses = false
	c.Display = NewConversationView("DuckDuckGo AI Chat CLI", "", false)
	c.coordinateRunner = func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		return "coordinate final response", nil
	}
	if err := c.SetNextCoordinate(coordinateTestPlan(CoordinateIndependent, 0)); err != nil {
		t.Fatal(err)
	}

	got, err := ptytest.Capture(master, slave, func() error {
		ProcessInput(c, "Question", cfg)
		c.Display.Redraw(80)
		return nil
	})
	if err != nil {
		t.Fatalf("capturing coordinate output failed: %v", err)
	}
	plain := strings.ReplaceAll(sgrSequence.ReplaceAllString(got, ""), "\r\n", "\n")
	framedAnswer := false
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "coordinate final response") && strings.Contains(line, "│") {
			framedAnswer = true
			break
		}
	}
	if !strings.Contains(plain, "╭─ Coordinate ─") || !framedAnswer || !strings.Contains(plain, "╰") {
		t.Fatalf("coordinate answer was not framed in initial output and redraw: %q", plain)
	}
}

func TestCoordinatedQuestionRejectsUnsupportedModelForHistoricalImage(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	c.Messages = []Message{{Role: "user", Content: "Earlier image", Images: []media.ImageAttachment{{Name: "old.png", Data: []byte("image")}}}}
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	calls := 0
	_, err := c.RunCoordinatedQuestion(context.Background(), plan, "Follow-up", cfg, func(_ context.Context, _ CoordinateAgent, _ string) (string, error) {
		calls++
		return "answer", nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot receive images") || calls != 0 {
		t.Fatalf("error=%v, calls=%d; historical image must not trigger silent model substitution", err, calls)
	}
}

func TestCoordinateCommitConsumesPendingSourcesOnUserTurn(t *testing.T) {
	c, _ := newCoordinateTestChat(t)
	source := chatcontext.SourceRef{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "notes.txt", URI: "/private/notes.txt", SizeBytes: 16, State: chatcontext.SourceStateActive}
	c.PendingSources = []chatcontext.SourceRef{source}
	run := &CoordinateRun{Complete: true, UserContent: "Summarize notes", FinalAnswer: "Summary"}

	if err := c.CommitCoordinatedQuestion("Summarize notes", run); err != nil {
		t.Fatalf("CommitCoordinatedQuestion() error = %v", err)
	}
	if len(c.PendingSources) != 0 {
		t.Fatalf("pending sources after commit = %+v, want consumed", c.PendingSources)
	}
	if len(c.Messages) != 2 || len(c.Messages[0].Sources) != 1 || c.Messages[0].Sources[0] != source || len(c.Messages[1].Sources) != 0 {
		t.Fatalf("coordinate messages = %+v, want sources on user turn only", c.Messages)
	}
}
