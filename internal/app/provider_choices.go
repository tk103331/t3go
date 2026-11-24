package app

import "github.com/tk/t3go/internal/proto"

func providerUsable(p proto.Provider) bool {
	return p.Installed && (p.Enabled || p.Source == "")
}

func (s *Session) providerAvailable(id string) bool {
	for _, p := range s.providers {
		if p.ID == id {
			return providerUsable(p)
		}
	}
	return false
}

func (s *Session) modelDisplayName(provider, id string) string {
	for _, model := range s.allModels() {
		if model.Provider == provider && model.ID == id && model.Name != "" {
			return model.Name
		}
	}
	return id
}

func (s *Session) defaultProviderModel(provider string) string {
	first := ""
	for _, model := range s.allModels() {
		if model.Provider != provider {
			continue
		}
		if model.Default {
			return model.ID
		}
		if first == "" {
			first = model.ID
		}
	}
	return first
}

// Preserve explicit defaults, including custom model IDs. An unavailable
// provider's model must not follow a fallback to another provider.
func (s *Session) newThreadDefaults() (provider, model string) {
	provider, model = s.settings.DefaultProvider, s.settings.DefaultModel
	if !s.providerAvailable(provider) {
		provider, model = "", ""
		for _, p := range s.providers {
			if providerUsable(p) {
				provider = p.ID
				break
			}
		}
	}
	if model == "" {
		model = s.defaultProviderModel(provider)
	}
	return
}
