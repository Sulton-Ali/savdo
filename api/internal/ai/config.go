package ai

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// DefaultMaxTokens is O-26's default per-request output cap, used
// whenever neither a Request nor a Config sets one. Not owner-configured
// via an environment variable — O-26 fixes it.
const DefaultMaxTokens = 1024

// Config selects a provider and model and prices its tokens for the cost
// estimate Usage.CostEstimate reports (O-28). The caller builds this from
// internal/config.Config's AIProvider/AIModel/AIPriceInputPerMTok/
// AIPriceOutputPerMTok fields — this package does not read the
// environment itself (every other module takes a typed config passed in
// by cmd/*, not its own env.Parse call). The one exception is the
// openai_compat provider's AI_BASE_URL/AI_API_KEY, read directly from the
// environment in openai_compat.go: they apply only to that one, optional,
// self-hosted provider, so they are not fields every Config value must
// carry.
type Config struct {
	// Provider selects the Client implementation: "anthropic" (built),
	// "openai_compat" (built) or "gemini" (registered, not built — O-29).
	Provider string
	// Model is passed to the provider as-is.
	Model string
	// PriceInputPerMTok and PriceOutputPerMTok are USD per million tokens,
	// decimal strings (ADR-007: no float money).
	PriceInputPerMTok  string
	PriceOutputPerMTok string
	// MaxTokens overrides DefaultMaxTokens when positive.
	MaxTokens int
}

// New builds the Client cfg.Provider names.
func New(cfg Config) (Client, error) {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}

	switch cfg.Provider {
	case "anthropic":
		return newAnthropicClient(cfg)
	case "openai_compat":
		return newOpenAICompatClient(cfg)
	case "gemini":
		// O-29: registered but unavailable — no google.golang.org/genai
		// dependency in this codebase.
		return nil, fmt.Errorf(`ai: provider "gemini" is not built: %w`, ErrProviderUnavailable)
	default:
		return nil, fmt.Errorf("ai: unknown provider %q: %w", cfg.Provider, ErrBadRequest)
	}
}

// parsePrices decimal-parses a Config's two per-million-token USD prices,
// wrapping a malformed value as ErrBadRequest — a bad AI_PRICE_* value is
// a misconfiguration, not a provider failure.
func parsePrices(cfg Config) (in, out decimal.Decimal, err error) {
	in, err = decimal.NewFromString(cfg.PriceInputPerMTok)
	if err != nil {
		return decimal.Decimal{}, decimal.Decimal{}, fmt.Errorf("ai: AI_PRICE_INPUT_PER_MTOK %q: %w: %v", cfg.PriceInputPerMTok, ErrBadRequest, err)
	}
	out, err = decimal.NewFromString(cfg.PriceOutputPerMTok)
	if err != nil {
		return decimal.Decimal{}, decimal.Decimal{}, fmt.Errorf("ai: AI_PRICE_OUTPUT_PER_MTOK %q: %w: %v", cfg.PriceOutputPerMTok, ErrBadRequest, err)
	}
	return in, out, nil
}

// costEstimate computes O-28's cost estimate: priceInPerMTok and
// priceOutPerMTok are USD per million tokens (decimal, ADR-007 — no
// float); the result is a 6-decimal-place USD string, NUMERIC(10,6)-
// compatible. E.g. prices 2.00/10.00 with 1500 input and 300 output
// tokens: (2.00*1500 + 10.00*300) / 1,000,000 = "0.006000".
func costEstimate(priceInPerMTok, priceOutPerMTok decimal.Decimal, inputTokens, outputTokens int64) string {
	perMTok := decimal.NewFromInt(1_000_000)
	in := priceInPerMTok.Mul(decimal.NewFromInt(inputTokens)).Div(perMTok)
	out := priceOutPerMTok.Mul(decimal.NewFromInt(outputTokens)).Div(perMTok)
	return in.Add(out).StringFixed(6)
}

// maxTokensOrDefault returns the per-request MaxTokens if the caller set
// one, else the provider's configured default, else DefaultMaxTokens.
func maxTokensOrDefault(requestMaxTokens, configMaxTokens int) int {
	if requestMaxTokens > 0 {
		return requestMaxTokens
	}
	if configMaxTokens > 0 {
		return configMaxTokens
	}
	return DefaultMaxTokens
}
