// Package workerplan names the standalone chat surface's input modes. Planning
// itself belongs to the shared worker decision loop.
package workerplan

type Mode string

const (
	ModeConversation     Mode = "conversation"
	ModeDirectExecution  Mode = "direct_execution"
	ModePlannedExecution Mode = "planned_execution"
)
