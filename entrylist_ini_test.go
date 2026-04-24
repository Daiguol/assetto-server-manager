package servermanager

import (
	"reflect"
	"testing"
)

func TestCleanGUIDs(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil input", input: nil, want: nil},
		{name: "empty slice", input: []string{}, want: nil},
		{name: "pure digits preserved", input: []string{"5463726354637263543"}, want: []string{"5463726354637263543"}},
		{name: "trailing whitespace stripped", input: []string{"5463726354637263543 "}, want: []string{"5463726354637263543"}},
		{name: "leading prefix stripped", input: []string{"S5463726354637263543"}, want: []string{"5463726354637263543"}},
		{name: "drops non-digit-only entries", input: []string{"abc"}, want: nil},
		{name: "mixed entries", input: []string{"abc123", "", "456def", "   ", "789"}, want: []string{"123", "456", "789"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanGUIDs(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
