package config

import (
	"os"
	"strconv"
	"strings"
	"unicode"
)

const (
	EnvLLMTemperature = "BIRDHACKBOT_LLM_TEMPERATURE"
	EnvLLMMaxTokens   = "BIRDHACKBOT_LLM_MAX_TOKENS"
)

func (cfg Config) ResolveLLMRoleOptions(role string, fallbackTemp float32, fallbackMaxTokens int) (float32, int) {
	temp := clampTemperature(fallbackTemp)
	maxTokens := clampMaxTokens(fallbackMaxTokens)

	if cfg.LLM.Temperature != nil {
		temp = clampTemperature(*cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens != nil {
		maxTokens = clampMaxTokens(*cfg.LLM.MaxTokens)
	}

	if roleCfg, ok := cfg.resolveRoleCfg(role); ok {
		if roleCfg.Temperature != nil {
			temp = clampTemperature(*roleCfg.Temperature)
		}
		if roleCfg.MaxTokens != nil {
			maxTokens = clampMaxTokens(*roleCfg.MaxTokens)
		}
	}

	if parsed, ok := readEnvFloat32(EnvLLMTemperature); ok {
		temp = clampTemperature(parsed)
	}
	if parsed, ok := readEnvInt(EnvLLMMaxTokens); ok {
		maxTokens = clampMaxTokens(parsed)
	}

	roleKey := roleToEnvKey(role)
	if roleKey != "" {
		if parsed, ok := readEnvFloat32("BIRDHACKBOT_LLM_" + roleKey + "_TEMPERATURE"); ok {
			temp = clampTemperature(parsed)
		}
		if parsed, ok := readEnvInt("BIRDHACKBOT_LLM_" + roleKey + "_MAX_TOKENS"); ok {
			maxTokens = clampMaxTokens(parsed)
		}
	}

	return temp, maxTokens
}

func (cfg Config) resolveRoleCfg(role string) (LLMRoleCfg, bool) {
	if len(cfg.LLM.Roles) == 0 {
		return LLMRoleCfg{}, false
	}
	want := normalizeRoleKey(role)
	if want == "" {
		return LLMRoleCfg{}, false
	}
	for key, value := range cfg.LLM.Roles {
		if normalizeRoleKey(key) == want {
			return value, true
		}
	}
	return LLMRoleCfg{}, false
}

func roleToEnvKey(role string) string {
	normalized := normalizeRoleKey(role)
	if normalized == "" {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(normalized, "-", "_"))
}

func normalizeRoleKey(role string) string {
	trimmed := strings.TrimSpace(strings.ToLower(role))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	out = strings.ReplaceAll(out, "__", "_")
	return out
}

func clampTemperature(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 2 {
		return 2
	}
	return v
}

func clampMaxTokens(v int) int {
	if v <= 0 {
		return 0
	}
	return v
}

func readEnvFloat32(key string) (float32, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return 0, false
	}
	return float32(parsed), true
}

func readEnvInt(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
