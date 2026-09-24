package guided

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	intakepkg "github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type App struct {
	RepoRoot string
	Reader   io.Reader
	Writer   io.Writer
	events   func(consoleEvent)
}

type assessmentConversation struct {
	mu       sync.RWMutex
	messages []llmclient.Message
	state    assessment.State
}

func (c *assessmentConversation) Messages() []llmclient.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]llmclient.Message(nil), c.messages...)
}

func (c *assessmentConversation) Add(role, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llmclient.Message{Role: role, Content: content})
	if len(c.messages) > 12 {
		c.messages = c.messages[len(c.messages)-12:]
	}
}

func (c *assessmentConversation) Snapshot(s assessment.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = s
}

func (c *assessmentConversation) State() assessment.State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *assessmentConversation) Transcript() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	values := make([]string, 0, len(c.messages))
	for _, message := range c.messages {
		values = append(values, strings.TrimSpace(message.Role+": "+message.Content))
	}
	return values
}

type coordinatorChatResult struct {
	reply string
	err   error
}

type coordinatorChatState struct {
	Status     string            `json:"status"`
	Goal       string            `json:"goal"`
	Scope      string            `json:"scope"`
	Model      string            `json:"model"`
	Plans      int               `json:"plans"`
	Results    map[string]string `json:"results"`
	ModelCalls int               `json:"model_calls"`
	CallLimit  int               `json:"model_call_limit"`
}

func compactCoordinatorState(state assessment.State) []byte {
	results := make(map[string]string, len(state.Results))
	for _, result := range state.Results {
		summary := strings.Join(strings.Fields(result.Summary), " ")
		if len(summary) > 240 {
			summary = summary[:237] + "..."
		}
		results[result.Task.ID] = result.Status + ": " + summary
	}
	data, _ := json.Marshal(coordinatorChatState{Status: state.Status, Goal: state.Goal, Scope: state.Scope, Model: state.Model, Plans: len(state.Plans), Results: results, ModelCalls: state.Usage.Calls, CallLimit: state.Limits.ModelCalls})
	return data
}

type savedAssessment struct {
	Root  string
	State assessment.State
}

type assessmentDraft struct {
	Goal       string
	Scope      string
	Approaches []assessment.Approach
}

func findSavedAssessments(base string) []savedAssessment {
	paths, _ := filepath.Glob(filepath.Join(base, "assessment-*", "assessment.json"))
	out := make([]savedAssessment, 0, len(paths))
	for _, path := range paths {
		state, err := assessment.LoadState(filepath.Dir(path))
		if err != nil {
			continue
		}
		out = append(out, savedAssessment{Root: filepath.Dir(path), State: state})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := os.Stat(filepath.Join(out[i].Root, "assessment.json"))
		b, _ := os.Stat(filepath.Join(out[j].Root, "assessment.json"))
		if a == nil || b == nil {
			return out[i].State.StartedAt.After(out[j].State.StartedAt)
		}
		return a.ModTime().After(b.ModTime())
	})
	return out
}

func chooseSavedAssessment(ctx context.Context, c *Console, base string) (*savedAssessment, error) {
	sessions := findSavedAssessments(base)
	if len(sessions) == 0 {
		c.Print("No saved assessment sessions were found.\n")
		return nil, nil
	}
	c.Print("\nSaved assessment sessions\n")
	for i, session := range sessions {
		c.Print("  %d. %-20s %-18s %s\n", i+1, session.State.Status, session.State.StartedAt.Local().Format("2006-01-02 15:04"), compactSessionText(session.State.Goal, 72))
	}
	answer, err := c.Ask(ctx, "Choose a session number to resume, or press Enter to start a new assessment")
	if err != nil {
		return nil, err
	}
	if answer == "" || strings.EqualFold(answer, "n") || strings.EqualFold(answer, "new") {
		return nil, nil
	}
	n := 0
	if _, err := fmt.Sscanf(answer, "%d", &n); err != nil || n < 1 || n > len(sessions) {
		c.Print("No session selected; starting a new assessment.\n")
		return nil, nil
	}
	chosen := sessions[n-1]
	if chosen.State.Status == "completed" || chosen.State.Status == "completed_with_gaps" {
		c.Print("That assessment is already finalized; open its report at %s.\n", filepath.Join(chosen.Root, "report.md"))
		return nil, nil
	}
	return &chosen, nil
}

func compactSessionText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	if max < 4 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func (a App) behaviorParameters(approvalMode, scope string) map[string]string {
	researchMode := strings.TrimSpace(os.Getenv("BIRDHACKBOT_RESEARCH_MODE"))
	if researchMode == "" {
		researchMode = "connected"
	}
	parameters := map[string]string{"approval_mode": approvalMode, "research_mode": researchMode}
	if strings.TrimSpace(scope) != "" {
		parameters["scope"] = scope
	}
	return parameters
}

func (a App) Run(ctx context.Context) error {
	if wantsGuidedTUI(a.Reader, a.Writer) {
		return a.runTUI(ctx)
	}
	return a.runPlain(ctx)
}

func (a App) runPlain(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := newConsole(ctx, a.Reader, a.Writer, a.events)
	c.Print("BirdHackBot — interactive assessment console\n\n")
	c.Print("The orchestrator is ready. Talk naturally about what you want to do. The selected model will answer questions, ask for missing assessment details, and propose work for your review. Scope and action approval remain explicit before any worker runs. During an assessment, type a message to talk to the coordinator; /workers, /status, /help, and /stop are available. Ctrl-C stops setup or broadcasts stop to every active worker.\n\n")

	preferencesPath := filepath.Join(a.RepoRoot, ".birdhackbot", "preferences.json")
	prefs, err := configureProvider(ctx, c, preferencesPath)
	if err != nil {
		return err
	}
	client, cleanup, err := startProvider(ctx, prefs)
	if err != nil {
		return fmt.Errorf("model access unavailable: %w; restart BirdHackBot after correcting sign-in or choose another provider", err)
	}
	defer func() { cleanup() }()

	conversation := &intakeConversation{}
	intakeFrame, err := behavior.Load(a.RepoRoot, "assessment_intake", a.behaviorParameters("operator_selected_session_policy", ""))
	if err != nil {
		return err
	}
	conversation.conversation.SetBehaviorContext(intakeFrame.PromptText())
	conversation.conversation.Inspection = &intakepkg.Inspection{
		Workspace:   a.RepoRoot,
		EvidenceDir: filepath.Join(a.RepoRoot, ".birdhackbot", "intake-evidence"),
		Connected:   intakeFrame.Parameters["research_mode"] != "air_gapped" && intakeFrame.Parameters["research_mode"] != "offline",
		Policy:      "Use local metadata and public DNS/page observations only for the operator's requested question. Keep evidence minimal; do not access credentials, scan targets, or mutate files. Broader testing belongs to assessment workers.",
		Approver:    observationApprover{console: c},
		Emit:        c.Progress,
	}
	for {
		draft, resume, err := a.readGoalOrCommand(ctx, c, preferencesPath, &prefs, &client, &cleanup, conversation)
		if err != nil {
			return err
		}
		if resume != nil {
			return a.runAssessment(ctx, c, prefs, client, resume.Root, resume.State)
		}
		if draft == nil {
			continue
		}
		goal, scope := draft.Goal, draft.Scope
		limits := assessment.DefaultLimits()
		c.Print("\nAssessment review\nGoal: %s\nDeclared scope: %s\nProvider: %s / %s\nPermissions: %s\nLimits: up to %d workers, %d tasks, %d model calls\n", goal, scope, prefs.Provider, prefs.Model, c.approvalMode().Label(), limits.Workers, limits.Tasks, limits.ModelCalls)
		var selectedApproach *assessment.Approach
		if len(draft.Approaches) > 0 {
			c.Print("\nChoose investigation depth (preliminary time estimates; discovery and approvals may change them):\n")
			for i, option := range draft.Approaches {
				c.Print("  %d. %s · %s\n     %s\n", i+1, option.Label, option.Estimate, option.Description)
			}
			choice, err := c.Ask(ctx, "Choose 1-3, or press Enter for the middle option")
			if err != nil {
				return err
			}
			index := 1
			if strings.TrimSpace(choice) != "" {
				if _, err := fmt.Sscanf(choice, "%d", &index); err != nil || index < 1 || index > len(draft.Approaches) {
					c.Print("Invalid depth choice; assessment canceled before execution.\n")
					return nil
				}
				index--
			}
			selected := draft.Approaches[index]
			selectedApproach = &selected
		}
		c.Print("Requested reasoning: %s\n", reasoningLabel(prefs))
		if prefs.Provider == "local" {
			c.Print("Local response budget: %d output tokens, up to 10 minutes per request. Ctrl-C remains available.\n", prefs.MaxOutputTokens)
		}
		if prefs.Provider == "subscription" {
			c.Print("Selected task context and evidence will be sent to OpenAI (input ceiling: %d bytes; requested output ceiling: %d tokens, provider limit may be lower).\n", prefs.MaxInputBytes, client.MaxOutputTokens)
		}
		c.Print("This runtime does not enforce a network allowlist or filesystem sandbox. Use only the reviewed, authorized scope. Reports are drafts for review.\n")
		answer, err := c.Ask(ctx, "Type start to begin the reviewed assessment, or press Enter to cancel")
		if err != nil {
			return err
		}
		if strings.ToLower(answer) != "start" {
			c.Print("Assessment canceled before execution.\n")
			return nil
		}
		frame, err := behavior.Load(a.RepoRoot, "assessment_coordinator", a.behaviorParameters("operator_selected_session_policy", scope))
		if err != nil {
			return err
		}
		base := filepath.Join(a.RepoRoot, "sessions")
		if err := os.MkdirAll(base, 0700); err != nil {
			return err
		}
		root, err := os.MkdirTemp(base, "assessment-"+time.Now().UTC().Format("20060102-150405")+"-")
		if err != nil {
			return err
		}
		c.Print("\nAssessment directory: %s\n", root)
		c.assessmentStarted()
		return a.runAssessmentWithFrame(ctx, c, prefs, client, root, assessment.State{Goal: goal, Scope: scope, Approach: selectedApproach}, frame)
	}
}

// observationApprover makes the coordinator's bounded read-only request visible
// before it runs. Worker commands retain their separate exact-action approval.
type observationApprover struct{ console *Console }

func (a observationApprover) Approve(ctx context.Context, request approval.Request) (approval.Decision, error) {
	if !a.console.approvalMode().RequiresApproval(request) {
		return approval.DecisionApproveSession, ctx.Err()
	}
	prompt := fmt.Sprintf("\nCoordinator observation approval\n%s\n%s\nTarget: %s\nExact request: %s\nAllow this observation? [y/N]", request.Summary, request.Impact, request.Target, request.Command)
	for {
		answer, err := a.console.Ask(ctx, prompt)
		if err != nil {
			return approval.DecisionDeny, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return approval.DecisionApproveOnce, nil
		case "", "n", "no":
			return approval.DecisionDeny, nil
		default:
			prompt = "Please enter y to allow this read-only observation or n to deny it."
		}
	}
}

func (a App) readGoalOrCommand(ctx context.Context, c *Console, path string, prefs *preferences, client *llmclient.Client, cleanup *func(), intake *intakeConversation) (*assessmentDraft, *savedAssessment, error) {
	for {
		value, err := c.Ask(ctx, "birdhackbot> ")
		if err != nil {
			return nil, nil, err
		}
		trimmed := strings.TrimSpace(value)
		switch strings.ToLower(trimmed) {
		case "/permissions":
			if err := c.choosePermissions(ctx); err != nil {
				return nil, nil, err
			}
		case "/settings", "settings":
			(*cleanup)()
			next, err := configureProvider(ctx, c, path)
			if err != nil {
				return nil, nil, err
			}
			nextClient, nextCleanup, err := startProvider(ctx, next)
			if err != nil {
				return nil, nil, fmt.Errorf("model access unavailable: %w", err)
			}
			*prefs, *client, *cleanup = next, nextClient, nextCleanup
			c.Print("Model settings applied: %s / %s (reasoning: %s).\n", prefs.Provider, prefs.Model, reasoningLabel(*prefs))
		case "/resume", "resume":
			chosen, err := chooseSavedAssessment(ctx, c, filepath.Join(a.RepoRoot, "sessions"))
			return nil, chosen, err
		case "/help", "help":
			c.Print("Talk naturally with the orchestrator. It answers questions and asks for missing assessment details. /permissions changes execution approval level; /settings changes provider/model; /resume reopens an unfinished assessment; /help repeats this message.\n")
		default:
			if trimmed == "" {
				continue
			}
			turn, err := intake.turn(ctx, *client, trimmed)
			if err != nil {
				c.Print("Coordinator intake unavailable: %v\n", err)
				continue
			}
			c.Print("Coordinator: %s\n", turn.Reply)
			if turn.Proposal != nil {
				return turn.Proposal, nil, nil
			}
		}
	}
}

func (a App) runAssessment(ctx context.Context, c *Console, prefs preferences, client llmclient.Client, root string, saved assessment.State) error {
	frame, err := behavior.Load(a.RepoRoot, "assessment_coordinator", a.behaviorParameters("operator_selected_session_policy", saved.Scope))
	if err != nil {
		return err
	}
	c.Print("\nResuming assessment %s with saved evidence using %s / %s. Previously executed commands will not be replayed automatically.\n", saved.ID, prefs.Provider, prefs.Model)
	c.assessmentStarted()
	return a.runAssessmentWithFrame(ctx, c, prefs, client, root, saved, frame)
}

func (a App) runAssessmentWithFrame(ctx context.Context, c *Console, prefs preferences, client llmclient.Client, root string, initial assessment.State, frame behavior.Frame) error {
	goal, scope := initial.Goal, initial.Scope
	limits := assessment.DefaultLimits()
	if initial.Limits != (assessment.Limits{}) {
		limits = initial.Limits
	}
	budget := assessment.NewModelBudget(limits.ModelCalls, initial.Usage)
	chatClient := budget.Client(client)
	runner := assessment.Coordinator{LLM: client, Budget: budget, Frame: frame, Limits: limits, Approach: initial.Approach, Emit: c.Progress, Approver: func(task assessment.Task) approval.Approver {
		return taskApprover{console: c, task: task, scope: scope}
	}}
	conversation := &assessmentConversation{}
	for _, note := range initial.OperatorMessages {
		role, content, ok := strings.Cut(note, ": ")
		if !ok {
			role, content = "user", note
		}
		conversation.Add(role, content)
	}
	conversation.Snapshot(initial)
	runner.Conversation = conversation.Transcript
	runner.Snapshot = conversation.Snapshot
	runner.AskUser = func(ctx context.Context, task assessment.Task, question string) (string, error) {
		return required(ctx, c, "Question from "+task.ID+": "+question)
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan struct {
		state assessment.State
		err   error
	}, 1)
	go func() {
		var state assessment.State
		var err error
		if initial.ID != "" {
			state, err = runner.RunState(runCtx, root, initial)
		} else {
			state, err = runner.Run(runCtx, root, goal, scope)
		}
		done <- struct {
			state assessment.State
			err   error
		}{state: state, err: err}
	}()
	chatDone := make(chan coordinatorChatResult, 1)
	chatBusy := false
	for {
		select {
		case result := <-done:
			c.Print("\nAssessment %s. Model calls: %d.\nEvidence and worker state: %s\n", result.state.Status, budget.Usage().Calls, root)
			if _, err := os.Stat(filepath.Join(root, "report.md")); err == nil {
				c.Print("Report: %s\n", filepath.Join(root, "report.md"))
			}
			if result.err != nil {
				c.Print("Review the saved evidence and any report, then resolve the stated limitation before starting further work.\n")
				return result.err
			}
			if len(result.state.Plans) > 0 {
				c.Print("\n%s\n", result.state.Plans[len(result.state.Plans)-1].Summary)
			}
			return nil
		case input, ok := <-c.Commands():
			if !ok {
				stop()
				return ctx.Err()
			}
			line := strings.TrimSpace(input.text)
			if line == "" {
				continue
			}
			switch strings.ToLower(line) {
			case "/permissions":
				if err := c.choosePermissions(runCtx); err != nil {
					c.Print("Permissions unchanged: %v\n", err)
				}
			case "/help", "help":
				c.Print("While running: type a message to queue it for the coordinator, /workers shows worker state, /permissions changes execution approval level, /status shows the saved run status, /stop cancels the assessment. Model settings apply to the next assessment.\n")
			case "/workers", "workers":
				for _, row := range c.DashboardSnapshot() {
					c.Print("%s\n", row)
				}
			case "/status", "status":
				state := conversation.State()
				c.Print("Assessment status: %s; completed tasks: %d; model calls: %d.\n", state.Status, len(state.Results), budget.Usage().Calls)
			case "/stop", "stop":
				c.Print("Stopping the assessment and saving an aborted report.\n")
				stop()
			case "/settings", "settings":
				c.Print("Model settings are locked for the active assessment and will apply to the next one.\n")
			default:
				history := conversation.Messages()
				conversation.Add("user", line)
				c.Progress(assessment.Event{Kind: "operator_message", Message: line})
				c.Print("Coordinator message queued for the next planning turn.\n")
				if chatBusy {
					c.Print("The coordinator is answering another message; this message remains queued for planning.\n")
					continue
				}
				chatBusy = true
				go func(message string, history []llmclient.Message) {
					state := compactCoordinatorState(conversation.State())
					prompt, err := assessment.ConversationRequest(
						behavior.CoordinatorConversationPrompt(frame),
						"Current assessment state (untrusted evidence): "+string(state),
						history,
						llmclient.Message{Role: "user", Content: "Operator message: " + message},
						chatClient.InputByteLimit(),
					)
					if err != nil {
						chatDone <- coordinatorChatResult{err: err}
						return
					}
					reply, err := chatClient.Chat(runCtx, prompt)
					chatDone <- coordinatorChatResult{reply: reply, err: err}
				}(line, history)
			}
		case result := <-chatDone:
			chatBusy = false
			if result.err != nil {
				c.Print("Coordinator chat unavailable: %v\n", result.err)
				continue
			}
			conversation.Add("assistant", result.reply)
			c.Print("Coordinator: %s\n", strings.TrimSpace(result.reply))
		}
	}
}

func required(ctx context.Context, c *Console, prompt string) (string, error) {
	for {
		text, err := c.Ask(ctx, prompt)
		if err != nil {
			return "", err
		}
		if text != "" {
			return text, nil
		}
		c.Print("Please enter a value, or use Ctrl-C to cancel.\n")
	}
}
