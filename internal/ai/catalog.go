/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ai

// ModelStatus indicates whether a model is actively supported.
type ModelStatus string

const (
	ModelStatusActive     ModelStatus = "active"
	ModelStatusDeprecated ModelStatus = "deprecated"
)

// ModelTier indicates what role a model is suited for.
type ModelTier string

const (
	ModelTierPrimary ModelTier = "primary"
	ModelTierExplain ModelTier = "explain"
	ModelTierBoth    ModelTier = "both"
)

// ModelEntry describes a single AI model available for use.
type ModelEntry struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Status      ModelStatus `json:"status"`
	PricingHint string      `json:"pricingHint,omitempty"`
	VerifiedAt  string      `json:"verifiedAt"`
	Tier        ModelTier   `json:"tier"`
}

// ModelCatalog maps provider names to their available models.
type ModelCatalog map[string][]ModelEntry

// DefaultCatalog returns the built-in model catalog with verified entries.
func DefaultCatalog() ModelCatalog {
	return ModelCatalog{
		ProviderNameAnthropic: {
			{
				ID:          "claude-haiku-4-5-20251001",
				Label:       "Claude Haiku 4.5",
				Status:      ModelStatusActive,
				PricingHint: "$0.80/MTok in, $4/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierBoth,
			},
			{
				ID:          "claude-sonnet-4-5-20250929",
				Label:       "Claude Sonnet 4.5",
				Status:      ModelStatusActive,
				PricingHint: "$3/MTok in, $15/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierPrimary,
			},
			{
				ID:          "claude-opus-4-6",
				Label:       "Claude Opus 4.6",
				Status:      ModelStatusActive,
				PricingHint: "$15/MTok in, $75/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierPrimary,
			},
			{
				ID:          "claude-3-5-haiku-20241022",
				Label:       "Claude 3.5 Haiku (Legacy)",
				Status:      ModelStatusDeprecated,
				PricingHint: "$0.80/MTok in, $4/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierExplain,
			},
		},
		ProviderNameOpenAI: {
			{
				ID:          "gpt-4o-mini",
				Label:       "GPT-4o Mini",
				Status:      ModelStatusActive,
				PricingHint: "$0.15/MTok in, $0.60/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierBoth,
			},
			{
				ID:          "gpt-4o",
				Label:       "GPT-4o",
				Status:      ModelStatusActive,
				PricingHint: "$2.50/MTok in, $10/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierPrimary,
			},
			{
				ID:          "gpt-4.1-mini",
				Label:       "GPT-4.1 Mini",
				Status:      ModelStatusActive,
				PricingHint: "$0.40/MTok in, $1.60/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierBoth,
			},
			{
				ID:          "gpt-4.1",
				Label:       "GPT-4.1",
				Status:      ModelStatusActive,
				PricingHint: "$2/MTok in, $8/MTok out",
				VerifiedAt:  "2026-02-08",
				Tier:        ModelTierPrimary,
			},
		},
	}
}

// ForProvider returns the models for a specific provider, or nil if not found.
func (c ModelCatalog) ForProvider(provider string) []ModelEntry {
	return c[provider]
}

// CostPer1KTokens returns the approximate blended cost per 1K tokens for a model.
// Falls back to provider-level defaults if the model is not in the catalog.
func CostPer1KTokens(provider, model string) float64 {
	catalog := DefaultCatalog()
	models := catalog[provider]
	for _, m := range models {
		if m.ID == model {
			return costForModel(provider, m.ID)
		}
	}
	return defaultProviderCost(provider)
}

func costForModel(provider, modelID string) float64 {
	// Blended input+output costs per 1K tokens
	costs := map[string]float64{
		// Anthropic
		"claude-haiku-4-5-20251001":  0.00125,
		"claude-3-5-haiku-20241022":  0.00125,
		"claude-sonnet-4-5-20250929": 0.005,
		"claude-opus-4-6":            0.025,
		// OpenAI
		"gpt-4o-mini":  0.00075,
		"gpt-4o":       0.00375,
		"gpt-4.1-mini": 0.001,
		"gpt-4.1":      0.003,
	}
	if c, ok := costs[modelID]; ok {
		return c
	}
	return defaultProviderCost(provider)
}

func defaultProviderCost(provider string) float64 {
	switch provider {
	case ProviderNameAnthropic:
		return 0.00125 // Haiku default
	case ProviderNameOpenAI:
		return 0.00075 // GPT-4o-mini default
	default:
		return 0
	}
}
