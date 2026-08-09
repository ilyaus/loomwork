package preset

import (
	"fmt"

	"github.com/ilyaus/loomwork/internal/provider"
)

// Parameter bounds enforced at load time so a bad config fails fast and legibly.
const (
	MinTemperature   = 0.0
	MaxTemperature   = 2.0
	MinTopP          = 0.0
	MaxTopP          = 1.0
	MinMaxOutTokens  = 1
	MinContextWindow = 1
)

func validateParams(params provider.Params, where string) error {
	if params.Temperature != nil {
		if *params.Temperature < MinTemperature || *params.Temperature > MaxTemperature {
			return fmt.Errorf("%s: temperature %.3f out of range [%.1f, %.1f]", where, *params.Temperature, MinTemperature, MaxTemperature)
		}
	}
	if params.TopP != nil {
		if *params.TopP < MinTopP || *params.TopP > MaxTopP {
			return fmt.Errorf("%s: top_p %.3f out of range [%.1f, %.1f]", where, *params.TopP, MinTopP, MaxTopP)
		}
	}
	if params.TopK != nil && *params.TopK < 0 {
		return fmt.Errorf("%s: top_k %d must be >= 0", where, *params.TopK)
	}
	if params.MaxOutputTokens != nil && *params.MaxOutputTokens < MinMaxOutTokens {
		return fmt.Errorf("%s: max_output_tokens %d must be >= %d", where, *params.MaxOutputTokens, MinMaxOutTokens)
	}
	if params.RepeatPenalty != nil && *params.RepeatPenalty < 0 {
		return fmt.Errorf("%s: repeat_penalty %.3f must be >= 0", where, *params.RepeatPenalty)
	}
	if params.ContextWindow != nil && *params.ContextWindow < MinContextWindow {
		return fmt.Errorf("%s: num_ctx %d must be >= %d", where, *params.ContextWindow, MinContextWindow)
	}
	return nil
}
