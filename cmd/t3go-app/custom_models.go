package main

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/egoist/asuka/sdk/store"
	"github.com/tk/t3go/internal/proto"
)

func customModelStoreKey(environmentID string) string {
	return "customModels:" + environmentID
}

// loadCustomModels reads model IDs the environment's CLIs accept without
// listing them. The old client kept one global map, so import it for the
// first environment that opens it.
func loadCustomModels(environmentID string) map[string][]proto.Model {
	if environmentID == "" {
		return map[string][]proto.Model{}
	}
	var models map[string][]proto.Model
	if store.Get(customModelStoreKey(environmentID), &models) {
		return cloneCustomModels(models)
	}
	var legacy string
	if !store.Get("customModels", &legacy) || json.Unmarshal([]byte(legacy), &models) != nil {
		return map[string][]proto.Model{}
	}
	models = cloneCustomModels(models)
	saveCustomModels(environmentID, models)
	return models
}

func saveCustomModels(environmentID string, models map[string][]proto.Model) {
	if environmentID == "" {
		return
	}
	_ = store.Set(customModelStoreKey(environmentID), models)
}

func cloneCustomModels(models map[string][]proto.Model) map[string][]proto.Model {
	next := make(map[string][]proto.Model, len(models))
	for provider, list := range models {
		next[provider] = slices.Clone(list)
	}
	return next
}

func (a *app) customModelList(provider string) []proto.Model {
	return slices.Clone(a.customModels.Get()[provider])
}

func (a *app) modelsForProvider(provider string) []proto.Model {
	var out []proto.Model
	seen := map[string]bool{}
	for _, model := range a.models.Peek() {
		if model.Provider == provider && !seen[model.ID] {
			seen[model.ID] = true
			out = append(out, model)
		}
	}
	for _, model := range a.customModelList(provider) {
		if !seen[model.ID] {
			seen[model.ID] = true
			model.Custom = true
			out = append(out, model)
		}
	}
	return out
}

func (a *app) openCustomModelDialog(provider string) {
	a.customModelFrom.Set(provider)
	a.customModelID.Set("")
	a.customModelName.Set("")
	a.customModelErr.Set("")
	a.showCustomModel.Set(true)
}

func (a *app) addCustomModel() {
	provider := strings.TrimSpace(a.customModelFrom.Peek())
	modelID := strings.TrimSpace(a.customModelID.Peek())
	name := strings.TrimSpace(a.customModelName.Peek())
	if provider == "" {
		a.customModelErr.Set("Choose a provider first.")
		return
	}
	if modelID == "" {
		a.customModelErr.Set("Enter the model ID the CLI expects.")
		return
	}
	if name == "" {
		name = modelID
	}
	models := cloneCustomModels(a.customModels.Peek())
	for _, model := range models[provider] {
		if model.ID == modelID {
			a.customModelErr.Set("That model is already listed.")
			return
		}
	}
	for _, model := range a.models.Peek() {
		if model.Provider == provider && model.ID == modelID {
			a.customModelErr.Set("That model is already reported by the provider.")
			return
		}
	}
	models[provider] = append(models[provider], proto.Model{
		ID: modelID, Name: name, Provider: provider, Custom: true,
	})
	a.customModels.Set(models)
	saveCustomModels(a.activeID, models)
	a.showCustomModel.Set(false)
}

func (a *app) removeCustomModel(provider, modelID string) {
	models := cloneCustomModels(a.customModels.Peek())
	list := models[provider]
	kept := make([]proto.Model, 0, len(list))
	for _, model := range list {
		if model.ID != modelID || !model.Custom {
			kept = append(kept, model)
		}
	}
	if len(kept) == 0 {
		delete(models, provider)
	} else {
		models[provider] = kept
	}
	a.customModels.Set(models)
	saveCustomModels(a.activeID, models)
}
