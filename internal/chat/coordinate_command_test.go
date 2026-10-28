package chat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"
)

type scriptedCoordinateSurvey struct {
	choices  []string
	inputs   []string
	panels   map[string][][]ui.PanelLine
	options  [][]string
	defaults []string
}

func (s *scriptedCoordinateSurvey) Panel(title string, lines []ui.PanelLine) {
	if s.panels == nil {
		s.panels = make(map[string][][]ui.PanelLine)
	}
	cloned := make([]ui.PanelLine, len(lines))
	for i := range lines {
		cloned[i] = append(ui.PanelLine(nil), lines[i]...)
	}
	s.panels[title] = append(s.panels[title], cloned)
}

func (s *scriptedCoordinateSurvey) Select(_ string, options []string, defaultChoice string) (string, error) {
	s.options = append(s.options, append([]string(nil), options...))
	s.defaults = append(s.defaults, defaultChoice)
	if len(s.choices) == 0 {
		return "", errors.New("unexpected coordinate menu selection")
	}
	choice := s.choices[0]
	s.choices = s.choices[1:]
	return choice, nil
}

func TestTerminalCoordinateArchivePromptDefaultsToNoSaveAndOffersCancel(t *testing.T) {
	for _, test := range []struct {
		choice string
		save   bool
		cancel bool
	}{
		{choice: "Save report", save: true},
		{choice: "Do not save"},
		{choice: "Cancel", cancel: true},
	} {
		t.Run(test.choice, func(t *testing.T) {
			prompts := &scriptedCoordinateSurvey{choices: []string{test.choice}}
			save, err := (terminalCoordinateArchivePrompt{prompts: prompts}).ChooseSave(coordinateArchiveTestRun(false))
			if test.cancel {
				if err == nil || save {
					t.Fatalf("cancel choice = save:%t error:%v, want canceled", save, err)
				}
			} else if err != nil || save != test.save {
				t.Fatalf("choice %q = save:%t error:%v", test.choice, save, err)
			}
			if len(prompts.defaults) != 1 || prompts.defaults[0] != "Do not save" {
				t.Fatalf("save choice default = %v, want Do not save", prompts.defaults)
			}
			if len(prompts.options) != 1 || !reflect.DeepEqual(prompts.options[0], []string{"Save report", "Do not save", "Cancel"}) {
				t.Fatalf("archive prompt options = %v", prompts.options)
			}
			if len(prompts.panels["Coordinate report privacy"]) != 1 {
				t.Fatal("archive prompt did not show what the report stores")
			}
		})
	}
}

func (s *scriptedCoordinateSurvey) Input(string) (string, error) {
	if len(s.inputs) == 0 {
		return "", errors.New("unexpected coordinate role input")
	}
	input := s.inputs[0]
	s.inputs = s.inputs[1:]
	return input, nil
}

func TestCoordinateMenuArmsDebateWithEditedRole(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	configHome := os.Getenv("XDG_CONFIG_HOME")
	prompts := &scriptedCoordinateSurvey{
		choices: []string{"Algorithm", "Debate", "Debate rounds", "2 rounds", "Execution", "Parallel independent steps", "Agents", "Agent 2", "Edit role", "Back", "Back", "Use for next question"},
		inputs:  []string{"Counterexample hunter"},
	}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	plan, ok := c.nextCoordinate()
	if !ok || plan.Strategy != CoordinateDebate || plan.DebateRounds != 2 || !plan.Parallel || len(plan.Agents) != 2 || plan.Agents[1].Role != "Counterexample hunter" {
		t.Fatalf("armed plan = %#v, ok=%v", plan, ok)
	}

	data, err := os.ReadFile(filepath.Join(configHome, "duckduckgo-chat-cli", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	loaded, ok := savedCoordinatePlan(&saved)
	if !ok || loaded.Strategy != CoordinateDebate || loaded.DebateRounds != 2 || !loaded.Parallel || len(loaded.Agents) != 2 || loaded.Agents[1].Role != "Counterexample hunter" {
		t.Fatalf("saved coordinate plan = %#v, ok=%v", loaded, ok)
	}
}

func TestCoordinateAgentEditsPersistBeforeArmingNextQuestion(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	configHome := os.Getenv("XDG_CONFIG_HOME")
	plan := coordinateTestPlan(CoordinateIndependent, 0)
	plan.Agents = plan.Agents[:2]
	var alternateModel models.Model
	for _, candidate := range models.Available() {
		if candidate.ID != plan.Agents[0].Model && candidate.ID != plan.Agents[1].Model {
			alternateModel = candidate.ID
			break
		}
	}
	if alternateModel == "" {
		t.Fatal("no alternate model available for persistence test")
	}
	cfg.Coordinate = config.CoordinateConfig{
		Strategy: string(plan.Strategy),
		Agents: []config.CoordinateAgentConfig{
			{Model: string(plan.Agents[0].Model), Role: plan.Agents[0].Role},
			{Model: string(plan.Agents[1].Model), Role: plan.Agents[1].Role},
		},
	}
	prompts := &scriptedCoordinateSurvey{
		choices: []string{"Agents", "Agent 2", "Change model", models.DisplayName(alternateModel), "Edit role", "Back", "Back", "Cancel"},
		inputs:  []string{"Persistent reviewer"},
	}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	if c.HasNextCoordinate() {
		t.Fatal("canceling after editing agents armed a coordinate run")
	}

	data, err := os.ReadFile(filepath.Join(configHome, "duckduckgo-chat-cli", "config.json"))
	if err != nil {
		t.Fatalf("edited agent preset was not saved before arming: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	loaded, ok := savedCoordinatePlan(&saved)
	if !ok || loaded.Agents[1].Model != alternateModel || loaded.Agents[1].Role != "Persistent reviewer" {
		t.Fatalf("saved coordinate preset = %#v, valid=%v; want persisted model and role edits", loaded, ok)
	}
}

func TestCoordinateMenuCancelDoesNotReplaceArmedPlan(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	initial := coordinateTestPlan(CoordinateChain, 0)
	if err := c.SetNextCoordinate(initial); err != nil {
		t.Fatal(err)
	}
	if err := runCoordinateMenu(c, cfg, &scriptedCoordinateSurvey{choices: []string{"Cancel"}}); err != nil {
		t.Fatal(err)
	}
	plan, ok := c.nextCoordinate()
	if !ok || plan.Strategy != CoordinateChain {
		t.Fatal("canceling menu discarded the prior armed selection")
	}
	c.CancelNextCoordinate()
	if c.HasNextCoordinate() {
		t.Fatal("explicit cancellation did not clear the armed selection")
	}
}

func TestCoordinateStrategyUpdatesOnlyAutomaticRoles(t *testing.T) {
	plan := CoordinatePlan{
		Strategy: CoordinateIndependent,
		Agents: []CoordinateAgent{
			{Role: defaultCoordinateRole(CoordinateIndependent, 0)},
			{Role: "Custom reviewer"},
			{Role: defaultCoordinateRole(CoordinateIndependent, 2)},
		},
	}
	setCoordinateStrategy(&plan, CoordinateDebate)
	if plan.Agents[0].Role != "Debater" || plan.Agents[1].Role != "Custom reviewer" || plan.Agents[2].Role != "Synthesizer" {
		t.Fatalf("roles after strategy change: %#v", plan.Agents)
	}
}

func TestCoordinateStrategiesKeepSeparateAgentModelsAndRoles(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	configHome := os.Getenv("XDG_CONFIG_HOME")
	cfg.Coordinate = config.CoordinateConfig{
		Strategy: string(CoordinateIndependent),
		Agents: []config.CoordinateAgentConfig{
			{Model: string(models.GPT6Luna), Role: "Legacy analyst"},
			{Model: string(models.GPT5Luna), Role: "Legacy verifier"},
		},
	}
	modelsByDefault := make([]models.Model, 0, 3)
	for _, candidate := range models.Available() {
		if candidate.ID != c.Model {
			modelsByDefault = append(modelsByDefault, candidate.ID)
		}
		if len(modelsByDefault) == 2 {
			break
		}
	}
	if len(modelsByDefault) < 2 {
		t.Fatal("need two alternate models to verify strategy-specific selections")
	}
	chainModel, debateModel := modelsByDefault[0], modelsByDefault[1]
	prompts := &scriptedCoordinateSurvey{
		choices: []string{
			"Algorithm", "Chain",
			"Agents", "Agent 1", "Change model", models.DisplayName(chainModel), "Edit role", "Chain reviewer", "Back", "Back",
			"Algorithm", "Debate",
			"Agents", "Agent 1", "Change model", models.DisplayName(debateModel), "Edit role", "Debate challenger", "Back", "Back",
			"Algorithm", "Chain", "Use for next question",
		},
		inputs: []string{"Chain reviewer", "Debate challenger"},
	}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	plan, ok := c.nextCoordinate()
	if !ok {
		t.Fatal("coordinate plan was not armed")
	}
	if plan.Strategy != CoordinateChain || len(plan.Agents) != 2 {
		t.Fatalf("restored strategy and agent count = %s, %d; want Chain with two agents", plan.Strategy, len(plan.Agents))
	}
	if plan.Agents[0].Model != chainModel || plan.Agents[0].Role != "Chain reviewer" {
		t.Fatalf("Chain profile = %#v; want its own model %q and role", plan.Agents[0], chainModel)
	}
	data, err := os.ReadFile(filepath.Join(configHome, "duckduckgo-chat-cli", "config.json"))
	if err != nil {
		t.Fatalf("strategy profiles were not saved: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		strategy CoordinateStrategy
		model    models.Model
		role     string
	}{
		{strategy: CoordinateIndependent, model: models.GPT6Luna, role: "Legacy analyst"},
		{strategy: CoordinateChain, model: chainModel, role: "Chain reviewer"},
		{strategy: CoordinateDebate, model: debateModel, role: "Debate challenger"},
	} {
		profile, ok := savedCoordinatePlanForStrategy(&saved, tc.strategy)
		if !ok || profile.Agents[0].Model != tc.model || profile.Agents[0].Role != tc.role {
			t.Fatalf("persisted %s profile = %#v, ok=%v; want model %q, role %q", tc.strategy, profile, ok, tc.model, tc.role)
		}
	}
}

func TestDefaultCoordinatePlanPrefillsTwoAgentsForEveryStrategy(t *testing.T) {
	c, _ := newCoordinateTestChat(t)
	for _, tc := range []struct {
		strategy CoordinateStrategy
		roles    []string
	}{
		{strategy: CoordinateIndependent, roles: []string{"Analyst", "Verifier"}},
		{strategy: CoordinateChain, roles: []string{"Solver", "Critic"}},
		{strategy: CoordinateDebate, roles: []string{"Debater", "Skeptic"}},
	} {
		plan := defaultCoordinatePlan(c, tc.strategy)
		if len(plan.Agents) != 2 {
			t.Fatalf("%s has %d default agents, want two", tc.strategy, len(plan.Agents))
		}
		for i, role := range tc.roles {
			if plan.Agents[i].Role != role || plan.Agents[i].Model == "" {
				t.Errorf("%s agent %d = %#v, want a prefilled model and role %q", tc.strategy, i+1, plan.Agents[i], role)
			}
		}
	}
}

func TestCoordinateMenuExplainsPlannedCallsAndParallelFanout(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	prompts := &scriptedCoordinateSurvey{choices: []string{"Execution", "Parallel independent steps", "Use for next question"}}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	panels := prompts.panels["Coordinate"]
	if len(panels) == 0 {
		t.Fatal("coordinate menu did not render its summary panel")
	}
	var summary string
	for _, line := range panels[len(panels)-1] {
		for _, segment := range line {
			summary += segment.Text
		}
		summary += "\n"
	}
	if !strings.Contains(summary, "3 planned Duck.ai calls") {
		t.Errorf("coordinate summary = %q, want planned request estimate", summary)
	}
	if !strings.Contains(summary, "Up to 2 simultaneous requests") {
		t.Errorf("coordinate summary = %q, want maximum parallel fan-out", summary)
	}
}

func TestCoordinateRecoveryOffersPartialOnlyWhenAnAgentOutputExists(t *testing.T) {
	cases := []struct {
		name        string
		stages      []CoordinateStage
		wantPartial bool
	}{
		{
			name: "available draft",
			stages: []CoordinateStage{
				{ID: "agent-1", Label: "Agent 1", Status: CoordinateStageCompleted, Response: "draft"},
				{ID: "agent-2", Label: "Agent 2", Status: CoordinateStageFailed, Err: "unavailable"},
			},
			wantPartial: true,
		},
		{
			name: "no usable draft",
			stages: []CoordinateStage{
				{ID: "agent-1", Label: "Agent 1", Status: CoordinateStageFailed, Err: "unavailable"},
				{ID: "agent-2", Label: "Agent 2", Status: CoordinateStageQueued},
			},
		},
		{
			name: "all agent outputs available",
			stages: []CoordinateStage{
				{ID: "agent-1", Label: "Agent 1", Status: CoordinateStageCompleted, Response: "draft one"},
				{ID: "agent-2", Label: "Agent 2", Status: CoordinateStageCompleted, Response: "draft two"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompts := &scriptedCoordinateSurvey{choices: []string{"Stop"}}
			action, err := (terminalCoordinateRecoveryPrompt{prompts: prompts}).ChooseRecovery(&CoordinateRun{Stages: tc.stages})
			if err != nil || action != CoordinateRecoveryStop {
				t.Fatalf("ChooseRecovery() = %q, %v", action, err)
			}
			if len(prompts.options) != 1 {
				t.Fatalf("recovery selections = %d, want one", len(prompts.options))
			}
			gotPartial := false
			for _, option := range prompts.options[0] {
				if option == "Synthesize partial result" {
					gotPartial = true
				}
			}
			if gotPartial != tc.wantPartial {
				t.Fatalf("options = %v, partial synthesis available = %v; want %v", prompts.options[0], gotPartial, tc.wantPartial)
			}
		})
	}
}

func TestCoordinateRecoveryPrefersFailedStageErrorToCancelledSibling(t *testing.T) {
	run := &CoordinateRun{Stages: []CoordinateStage{
		{ID: "agent-1", Status: CoordinateStageCancelled, Err: "context canceled"},
		{ID: "agent-2", Status: CoordinateStageFailed, Err: "429 Too Many Requests"},
	}}

	err := firstCoordinateStageError(run)
	if err == nil || !strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("firstCoordinateStageError() = %v, want the failed agent's 429 error", err)
	}
}

func TestCoordinateStrategyMenuExplainsEachAlgorithm(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	prompts := &scriptedCoordinateSurvey{choices: []string{"Algorithm", "Back", "Cancel"}}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	for _, panels := range prompts.panels {
		for _, panel := range panels {
			for _, line := range panel {
				for _, segment := range line {
					rendered.WriteString(segment.Text)
				}
				rendered.WriteByte('\n')
			}
		}
	}
	for _, description := range []string{
		"Two separate answers, then a synthesis.",
		"Agent 2 critiques Agent 1's draft before synthesis.",
		"Agents revise against each other for one or two rounds.",
	} {
		if !strings.Contains(rendered.String(), description) {
			t.Errorf("algorithm panels do not include %q; panels: %s", description, rendered.String())
		}
	}
}

func TestCoordinateMenuRequiresReplacingUnavailableSavedModel(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg.Coordinate = config.CoordinateConfig{
		Strategy:     string(CoordinateDebate),
		DebateRounds: 2,
		Parallel:     true,
		Agents: []config.CoordinateAgentConfig{
			{Model: string(models.GPT6Luna), Role: "Primary researcher"},
			{Model: "removed-model-id", Role: "Counterexample hunter"},
		},
	}
	var availableModel models.Model
	for _, candidate := range models.Available() {
		if candidate.ID != models.GPT6Luna {
			availableModel = candidate.ID
			break
		}
	}
	prompts := &scriptedCoordinateSurvey{choices: []string{
		"Use for next question", "Agents", "Agent 2", "Change model",
		models.DisplayName(availableModel), "Back", "Back", "Use for next question",
	}}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	plan, ok := c.nextCoordinate()
	if !ok || plan.Strategy != CoordinateDebate || plan.DebateRounds != 2 || !plan.Parallel {
		t.Fatalf("repaired saved plan = %#v, armed=%v", plan, ok)
	}
	if plan.Agents[0].Model != models.GPT6Luna || plan.Agents[1].Model != availableModel || plan.Agents[1].Role != "Counterexample hunter" {
		t.Fatalf("replacing stale model changed unrelated settings: %#v", plan.Agents)
	}
	var initialPanel string
	for _, line := range prompts.panels["Coordinate"][0] {
		for _, segment := range line {
			initialPanel += segment.Text
		}
	}
	if !strings.Contains(strings.ToLower(initialPanel), "unavailable") {
		t.Fatalf("stale model was not identified in the main menu: %q", initialPanel)
	}
}

func TestCoordinateMenuAllowsDuplicateModelsWithoutAdvisory(t *testing.T) {
	c, cfg := newCoordinateTestChat(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg.Coordinate = config.CoordinateConfig{
		Strategy:     string(CoordinateIndependent),
		DebateRounds: 1,
		Agents: []config.CoordinateAgentConfig{
			{Model: string(models.GPT6Luna), Role: "Researcher"},
			{Model: string(models.GPT6Luna), Role: "Researcher"},
		},
	}
	prompts := &scriptedCoordinateSurvey{choices: []string{"Use for next question"}}
	if err := runCoordinateMenu(c, cfg, prompts); err != nil {
		t.Fatal(err)
	}
	plan, ok := c.nextCoordinate()
	if !ok || plan.Agents[0].Model != plan.Agents[1].Model || plan.Agents[0].Role != plan.Agents[1].Role {
		t.Fatalf("duplicate selections were not preserved: %#v, armed=%v", plan.Agents, ok)
	}
	var summary string
	for _, line := range prompts.panels["Coordinate"][0] {
		for _, segment := range line {
			summary += segment.Text
		}
	}
	if strings.Contains(strings.ToLower(summary), "duplicate") {
		t.Fatalf("same-model agents unexpectedly triggered duplicate advisory: %q", summary)
	}
}

func TestCoordinateDuplicateAdvisoryForRepeatedRoleAcrossDifferentModels(t *testing.T) {
	plan := CoordinatePlan{Agents: []CoordinateAgent{
		{Model: models.GPT6Luna, Role: "Researcher"},
		{Model: models.GPT5Luna, Role: " researcher "},
	}}
	if advisory := coordinateDuplicateAdvisory(plan); advisory == "" {
		t.Fatal("repeated role across different models should trigger advisory")
	}
}
