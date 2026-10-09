package guided

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/webapp"
)

// chooseConfiguredProfile uses the same model choices as the web UI. A custom
// setup remains available when no profiles file exists or the operator needs
// an endpoint that has not been configured yet.
func chooseConfiguredProfile(ctx context.Context, c *Console, preferencesPath string) (preferences, bool, error) {
	repoRoot := filepath.Dir(filepath.Dir(preferencesPath))
	profilesPath := filepath.Join(repoRoot, "config", "model-profiles.local.json")
	if _, err := os.Stat(profilesPath); os.IsNotExist(err) {
		return preferences{}, false, nil
	} else if err != nil {
		return preferences{}, false, err
	}
	file, err := webapp.LoadModelProfiles(profilesPath)
	if err != nil {
		return preferences{}, false, fmt.Errorf("load configured models: %w", err)
	}
	defaultChoice := 0
	var saved preferences
	if data, err := os.ReadFile(preferencesPath); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	choices := make([]consoleChoice, 0, len(file.Profiles)+1)
	for i, profile := range file.Profiles {
		if profile.ID == file.Default {
			defaultChoice = i
		}
		provider := "Local model"
		if profile.Provider == "subscription" {
			provider = "OpenAI subscription"
		}
		choices = append(choices, consoleChoice{Label: profile.Label, Detail: profile.Model + " · " + provider, Value: profile.ID})
	}
	for i, profile := range file.Profiles {
		if saved.Provider == profile.Provider && saved.Model == profile.Model && saved.BaseURL == profile.BaseURL {
			defaultChoice = i
			break
		}
	}
	choices = append(choices, consoleChoice{Label: "Set up another model", Detail: "Enter an endpoint and model manually", Value: "custom"})
	for {
		answer, err := c.Choose(ctx, "Configured models", "Choose a model, or Esc to close", choices, defaultChoice)
		if err != nil {
			return preferences{}, false, err
		}
		if strings.EqualFold(answer, "custom") || strings.EqualFold(answer, "c") {
			return preferences{}, false, nil
		}
		for _, profile := range file.Profiles {
			if profile.ID == answer {
				return preferences{
					Provider:              profile.Provider,
					BaseURL:               profile.BaseURL,
					Model:                 profile.Model,
					TokenFile:             profile.TokenFile,
					ManagedBridge:         profile.ManagedBridge,
					ReasoningEffort:       profile.ReasoningEffort,
					StructuredJSON:        profile.StructuredJSON,
					MaxOutputTokens:       profile.MaxOutputTokens,
					MaxInputBytes:         profile.MaxInputBytes,
					RequestTimeoutSeconds: profile.RequestTimeoutSeconds,
				}, true, nil
			}
		}
		c.Print("Choose a listed model.\n")
	}
}
