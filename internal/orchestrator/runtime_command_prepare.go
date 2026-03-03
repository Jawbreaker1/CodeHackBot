package orchestrator

import "fmt"

type runtimeCommandPreparation struct {
	Command                string
	Args                   []string
	InjectedInitialTarget  bool
	InitialTarget          string
	Adapted                bool
	AdaptationNote         string
	InjectedFollowupTarget bool
	FollowupTarget         string
}

func prepareRuntimeCommand(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) runtimeCommandPreparation {
	result := runtimeCommandPreparation{
		Command: command,
		Args:    args,
	}
	result.Args, result.InjectedInitialTarget, result.InitialTarget = applyCommandTargetFallback(scopePolicy, task, result.Command, result.Args)
	result.Command, result.Args, result.AdaptationNote, result.Adapted = adaptCommandForRuntime(scopePolicy, result.Command, result.Args)
	result.Args, result.InjectedFollowupTarget, result.FollowupTarget = applyCommandTargetFallback(scopePolicy, task, result.Command, result.Args)
	return result
}

func runtimePreparationMessages(prepared runtimeCommandPreparation) []string {
	messages := make([]string, 0, 3)
	if prepared.InjectedInitialTarget {
		messages = append(messages, fmt.Sprintf("auto-injected target %s for command %s", prepared.InitialTarget, prepared.Command))
	}
	if prepared.Adapted && prepared.AdaptationNote != "" {
		messages = append(messages, prepared.AdaptationNote)
	}
	if prepared.InjectedFollowupTarget {
		messages = append(messages, fmt.Sprintf("auto-injected target %s for command %s after runtime adaptation", prepared.FollowupTarget, prepared.Command))
	}
	return messages
}
