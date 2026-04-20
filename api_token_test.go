package servermanager

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerTokenMiddleware(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name       string
		tokens     []string
		header     string
		wantStatus int
		wantCalled bool
	}{
		{"no tokens configured", nil, "Bearer anything", http.StatusUnauthorized, false},
		{"empty tokens after trim", []string{"  "}, "Bearer anything", http.StatusUnauthorized, false},
		{"missing header", []string{"secret"}, "", http.StatusUnauthorized, false},
		{"wrong scheme", []string{"secret"}, "Basic secret", http.StatusUnauthorized, false},
		{"empty bearer", []string{"secret"}, "Bearer ", http.StatusUnauthorized, false},
		{"wrong token", []string{"secret"}, "Bearer nope", http.StatusUnauthorized, false},
		{"valid token", []string{"secret"}, "Bearer secret", http.StatusOK, true},
		{"valid token among many", []string{"a", "b", "secret"}, "Bearer secret", http.StatusOK, true},
		{"valid token with surrounding spaces", []string{"secret"}, "Bearer    secret  ", http.StatusOK, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			handler := BearerTokenMiddleware(tc.tokens)(next)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/server/state", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if called != tc.wantCalled {
				t.Fatalf("next called = %v, want %v", called, tc.wantCalled)
			}
		})
	}
}

func TestParseSessionTypeFromFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2024_4_20_15_30_RACE.json", "RACE"},
		{"2024_4_20_15_30_QUALIFY.json", "QUALIFY"},
		{"2024_4_20_15_30_PRACTICE.json", "PRACTICE"},
		{"no_extension_RACE", "RACE"},
		{"single", ""},
		{"", ""},
	}

	for _, tc := range cases {
		got := parseSessionTypeFromFilename(tc.in)
		if got != tc.want {
			t.Errorf("parseSessionTypeFromFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitCars(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"car_one", []string{"car_one"}},
		{"car_one;car_two;car_three", []string{"car_one", "car_two", "car_three"}},
		{"car_one; car_two ;  ;car_three", []string{"car_one", "car_two", "car_three"}},
	}

	for _, tc := range cases {
		got := splitCars(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCars(%q) length = %d, want %d (got %v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCars(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
