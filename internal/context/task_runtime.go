package context

// TaskRuntime records lifecycle, not a guessed interpretation of the task.
// CurrentTarget and MissingFact remain readable in older saved sessions; new
// workers derive neither from command text nor from regex matches in the goal.
type TaskRuntime struct {
	State         string
	CurrentTarget string
	MissingFact   string
}
