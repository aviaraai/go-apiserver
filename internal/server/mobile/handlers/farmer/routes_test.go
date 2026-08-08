package farmer

import "testing"

func TestOptionalField(t *testing.T) {
	blank := ""
	spaces := "   "
	padded := "  son  "
	value := "wife"

	tests := []struct {
		name string
		in   *string
		want *string
	}{
		{"nil stays nil", nil, nil},
		{"empty becomes nil", &blank, nil},
		{"whitespace becomes nil", &spaces, nil},
		{"value is trimmed", &padded, strPtr("son")},
		{"value is kept", &value, strPtr("wife")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionalField(tt.in)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("got %q, want nil", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("got nil, want %q", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("got %q, want %q", *got, *tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
