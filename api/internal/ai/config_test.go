package ai

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestCostEstimate(t *testing.T) {
	tests := []struct {
		name                string
		priceIn, priceOut   string
		inputTok, outputTok int64
		want                string
	}{
		{
			name:    "claude-sonnet-5 defaults: 1500 in + 300 out",
			priceIn: "2.00", priceOut: "10.00",
			inputTok: 1500, outputTok: 300,
			want: "0.006000",
		},
		{
			name:    "zero tokens cost nothing",
			priceIn: "2.00", priceOut: "10.00",
			inputTok: 0, outputTok: 0,
			want: "0.000000",
		},
		{
			name:    "input only",
			priceIn: "2.00", priceOut: "10.00",
			inputTok: 1_000_000, outputTok: 0,
			want: "2.000000",
		},
		{
			name:    "output only",
			priceIn: "2.00", priceOut: "10.00",
			inputTok: 0, outputTok: 1_000_000,
			want: "10.000000",
		},
		{
			name:    "sub-cent amounts round to six places",
			priceIn: "0.15", priceOut: "0.60",
			inputTok: 37, outputTok: 11,
			want: "0.000012", // (0.15*37 + 0.60*11)/1e6 = (5.55+6.6)/1e6 = 0.00001215 -> rounds to 0.000012
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := decimal.RequireFromString(tt.priceIn)
			out := decimal.RequireFromString(tt.priceOut)
			got := costEstimate(in, out, tt.inputTok, tt.outputTok)
			if got != tt.want {
				t.Fatalf("costEstimate(%s, %s, %d, %d) = %q, want %q", tt.priceIn, tt.priceOut, tt.inputTok, tt.outputTok, got, tt.want)
			}
		})
	}
}

func TestMaxTokensOrDefault(t *testing.T) {
	tests := []struct {
		name                              string
		requestMaxTokens, configMaxTokens int
		want                              int
	}{
		{"request wins", 500, 2000, 500},
		{"falls back to config", 0, 2000, 2000},
		{"falls back to package default", 0, 0, DefaultMaxTokens},
		{"negative request falls back to config", -1, 2000, 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxTokensOrDefault(tt.requestMaxTokens, tt.configMaxTokens)
			if got != tt.want {
				t.Fatalf("maxTokensOrDefault(%d, %d) = %d, want %d", tt.requestMaxTokens, tt.configMaxTokens, got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	basePrices := Config{PriceInputPerMTok: "2.00", PriceOutputPerMTok: "10.00", Model: "claude-sonnet-5"}

	t.Run("anthropic provider builds a Client", func(t *testing.T) {
		cfg := basePrices
		cfg.Provider = "anthropic"
		client, err := New(cfg)
		if err != nil {
			t.Fatalf("New(anthropic): %v", err)
		}
		if client == nil {
			t.Fatal("New(anthropic) returned a nil Client")
		}
	})

	t.Run("openai_compat provider requires AI_BASE_URL", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "")
		cfg := basePrices
		cfg.Provider = "openai_compat"
		_, err := New(cfg)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("New(openai_compat, no AI_BASE_URL) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})

	t.Run("openai_compat provider builds a Client when AI_BASE_URL is set", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "http://localhost:11434/v1")
		cfg := basePrices
		cfg.Provider = "openai_compat"
		client, err := New(cfg)
		if err != nil {
			t.Fatalf("New(openai_compat): %v", err)
		}
		if client == nil {
			t.Fatal("New(openai_compat) returned a nil Client")
		}
	})

	t.Run("gemini is registered but not built (O-29)", func(t *testing.T) {
		cfg := basePrices
		cfg.Provider = "gemini"
		_, err := New(cfg)
		if !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("New(gemini) = %v, want errors.Is(_, ErrProviderUnavailable)", err)
		}
	})

	t.Run("unknown provider is a bad request", func(t *testing.T) {
		cfg := basePrices
		cfg.Provider = "made_up"
		_, err := New(cfg)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("New(made_up) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})

	t.Run("a malformed price is a bad request", func(t *testing.T) {
		cfg := Config{Provider: "anthropic", Model: "claude-sonnet-5", PriceInputPerMTok: "not-a-number", PriceOutputPerMTok: "10.00"}
		_, err := New(cfg)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("New(bad price) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})
}
