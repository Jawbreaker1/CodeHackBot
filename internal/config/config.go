package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Agent struct {
		Name         string `json:"name"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
		MaxSteps     int    `json:"max_steps"`
		ToolMaxFixes int    `json:"tool_max_fixes"`
		ToolMaxFiles int    `json:"tool_max_files"`
	} `json:"agent"`
	LLM struct {
		BaseURL         string                `json:"base_url"`
		Model           string                `json:"model"`
		TimeoutSeconds  int                   `json:"timeout_seconds"`
		APIKey          string                `json:"api_key"`
		MaxFailures     int                   `json:"max_failures"`
		CooldownSeconds int                   `json:"cooldown_seconds"`
		Temperature     *float32              `json:"temperature,omitempty"`
		MaxTokens       *int                  `json:"max_tokens,omitempty"`
		Roles           map[string]LLMRoleCfg `json:"roles,omitempty"`
	} `json:"llm"`
	Permissions struct {
		Level           string `json:"level"`
		RequireApproval bool   `json:"require_approval"`
	} `json:"permissions"`
	Scope struct {
		Networks    []string `json:"networks"`
		Targets     []string `json:"targets"`
		DenyTargets []string `json:"deny_targets"`
	} `json:"scope"`
	Context struct {
		MaxRecentOutputs   int `json:"max_recent_outputs"`
		SummarizeEvery     int `json:"summarize_every_steps"`
		SummarizeAtPercent int `json:"summarize_at_percent"`
		ChatHistoryLines   int `json:"chat_history_lines"`
		PlaybookMax        int `json:"playbook_max"`
		PlaybookLines      int `json:"playbook_lines"`
	} `json:"context"`
	Session struct {
		LedgerEnabled     bool   `json:"ledger_enabled"`
		LogDir            string `json:"log_dir"`
		PlanFilename      string `json:"plan_filename"`
		InventoryFilename string `json:"inventory_filename"`
		LedgerFilename    string `json:"ledger_filename"`
	} `json:"session"`
	Tools struct {
		Shell struct {
			Enabled        bool `json:"enabled"`
			TimeoutSeconds int  `json:"timeout_seconds"`
		} `json:"shell"`
		Metasploit struct {
			DiscoveryMode string `json:"discovery_mode"`
			RPCEnabled    bool   `json:"rpc_enabled"`
		} `json:"metasploit"`
	} `json:"tools"`
	Network struct {
		AssumeOffline bool `json:"assume_offline"`
	} `json:"network"`
	UI struct {
		Verbose bool `json:"verbose"`
	} `json:"ui"`
}

type LLMRoleCfg struct {
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

func DefaultPath() string {
	return filepath.Join("config", "default.json")
}

func ProfilePath(profile string) string {
	return filepath.Join("config", "profiles", profile+".json")
}

func SessionPath(logDir, sessionID string) string {
	return filepath.Join(logDir, sessionID, "config.json")
}

func Load(defaultPath, profilePath, sessionPath string) (Config, []string, error) {
	paths := []string{}
	merged := map[string]any{}

	if defaultPath == "" {
		defaultPath = DefaultPath()
	}
	if err := mergeFile(merged, defaultPath, true); err != nil {
		return Config{}, paths, err
	}
	paths = append(paths, defaultPath)

	if profilePath != "" {
		if err := mergeFile(merged, profilePath, true); err != nil {
			return Config{}, paths, err
		}
		paths = append(paths, profilePath)
	}

	if sessionPath != "" {
		if err := mergeFile(merged, sessionPath, true); err != nil {
			return Config{}, paths, err
		}
		paths = append(paths, sessionPath)
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return Config{}, paths, fmt.Errorf("marshal merged config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, paths, fmt.Errorf("unmarshal merged config: %w", err)
	}

	return cfg, paths, nil
}

func mergeFile(dst map[string]any, path string, required bool) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return fmt.Errorf("config file not found: %s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("config path is a directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %s: %w", path, err)
	}
	var src map[string]any
	if err := json.Unmarshal(data, &src); err != nil {
		return fmt.Errorf("parse config: %s: %w", path, err)
	}
	deepMerge(dst, src)
	return nil
}

func deepMerge(dst, src map[string]any) {
	for key, value := range src {
		srcMap, ok := value.(map[string]any)
		if !ok {
			dst[key] = value
			continue
		}
		if existing, ok := dst[key]; ok {
			if existingMap, ok := existing.(map[string]any); ok {
				deepMerge(existingMap, srcMap)
				continue
			}
		}
		newMap := map[string]any{}
		deepMerge(newMap, srcMap)
		dst[key] = newMap
	}
}

func Save(path string, cfg Config) error {
	if path == "" {
		return fmt.Errorf("config path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
