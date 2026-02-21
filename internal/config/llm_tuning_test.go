package config

import "testing"

func TestResolveLLMRoleOptionsPrecedence(t *testing.T) {
	t.Setenv(EnvLLMTemperature, "")
	t.Setenv(EnvLLMMaxTokens, "")
	t.Setenv("BIRDHACKBOT_LLM_ASSIST_TEMPERATURE", "")
	t.Setenv("BIRDHACKBOT_LLM_ASSIST_MAX_TOKENS", "")

	baseTemp := float32(0.25)
	baseTokens := 900
	roleTemp := float32(0.11)
	roleTokens := 350
	cfg := Config{}
	cfg.LLM.Temperature = &baseTemp
	cfg.LLM.MaxTokens = &baseTokens
	cfg.LLM.Roles = map[string]LLMRoleCfg{
		"assist": {
			Temperature: &roleTemp,
			MaxTokens:   &roleTokens,
		},
	}

	temp, maxTokens := cfg.ResolveLLMRoleOptions("assist", 0.7, 1200)
	if temp != roleTemp || maxTokens != roleTokens {
		t.Fatalf("expected role override (%v,%d), got (%v,%d)", roleTemp, roleTokens, temp, maxTokens)
	}
}

func TestResolveLLMRoleOptionsEnvOverrides(t *testing.T) {
	t.Setenv(EnvLLMTemperature, "0.3")
	t.Setenv(EnvLLMMaxTokens, "800")
	t.Setenv("BIRDHACKBOT_LLM_PLANNER_TEMPERATURE", "0.05")
	t.Setenv("BIRDHACKBOT_LLM_PLANNER_MAX_TOKENS", "1600")

	cfg := Config{}
	temp, maxTokens := cfg.ResolveLLMRoleOptions("planner", 0.2, 1000)
	if temp != float32(0.05) {
		t.Fatalf("expected role env temperature 0.05, got %v", temp)
	}
	if maxTokens != 1600 {
		t.Fatalf("expected role env max tokens 1600, got %d", maxTokens)
	}
}

func TestResolveLLMRoleOptionsClampsValues(t *testing.T) {
	t.Setenv(EnvLLMTemperature, "")
	t.Setenv(EnvLLMMaxTokens, "")

	baseTemp := float32(9.5)
	baseTokens := -10
	cfg := Config{}
	cfg.LLM.Temperature = &baseTemp
	cfg.LLM.MaxTokens = &baseTokens
	temp, maxTokens := cfg.ResolveLLMRoleOptions("assist", -2, -5)
	if temp != 2 {
		t.Fatalf("expected clamped temperature 2, got %v", temp)
	}
	if maxTokens != 0 {
		t.Fatalf("expected clamped max tokens 0, got %d", maxTokens)
	}
}
