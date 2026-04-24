package servermanager

import (
	"testing"
	"time"
)

func TestGetResultDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "happy path (RACE)",
			input: "2019_2_15_21_16_RACE.json",
			want:  time.Date(2019, 2, 15, 21, 16, 0, 0, time.Local),
		},
		{
			name:  "happy path (PRACTICE)",
			input: "2019_3_2_14_41_PRACTICE.json",
			want:  time.Date(2019, 3, 2, 14, 41, 0, 0, time.Local),
		},
		{
			name:    "too few parts",
			input:   "2019_2_RACE.json",
			wantErr: true,
		},
		{
			name:    "non-numeric parts",
			input:   "abc_def_ghi_jkl_mno_RACE.json",
			wantErr: true,
		},
		{
			name:    "single token",
			input:   "garbage.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetResultDate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value %v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormValueAsInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "on maps to 1", input: "on", want: 1},
		{name: "numeric", input: "42", want: 42},
		{name: "zero", input: "0", want: 0},
		{name: "negative", input: "-5", want: -5},
		{name: "empty string", input: "", want: 0},
		{name: "garbage", input: "abc", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formValueAsInt(tt.input); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		trim bool
		want string
	}{
		{name: "zero", d: 0, want: "00:00:00.000"},
		{name: "500ms", d: 500 * time.Millisecond, want: "00:00:00.500"},
		{name: "1min30s", d: 90 * time.Second, want: "00:01:30.000"},
		{name: "1h2m3s", d: time.Hour + 2*time.Minute + 3*time.Second, want: "01:02:03.000"},
		{name: "trim removes leading 00:", d: 90 * time.Second, trim: true, want: "01:30.000"},
		{name: "trim has no effect with hours", d: time.Hour + 5*time.Minute, trim: true, want: "01:05:00.000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.d, tt.trim); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormaliseEntrantGUID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "single", input: "76561198020046073", want: "76561198020046073"},
		{name: "already sorted", input: "111;222;333", want: "111;222;333"},
		{name: "reverse order gets sorted", input: "333;222;111", want: "111;222;333"},
		{name: "drops empties between separators", input: "222;;111", want: "111;222"},
		{name: "strips non-digits before sorting", input: "steam_333;steam_111", want: "111;333"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormaliseEntrantGUID(tt.input); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
