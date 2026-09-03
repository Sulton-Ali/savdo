package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults when nothing set",
			env:  map[string]string{},
			want: Config{
				Addr:     ":8080",
				LogLevel: "info",
			},
		},
		{
			name: "env overrides defaults",
			env: map[string]string{
				"API_ADDR":     ":9090",
				"LOG_LEVEL":    "debug",
				"DATABASE_URL": "postgres://savdo:savdo@localhost:5432/savdo?sslmode=disable",
			},
			want: Config{
				Addr:        ":9090",
				LogLevel:    "debug",
				DatabaseURL: "postgres://savdo:savdo@localhost:5432/savdo?sslmode=disable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
