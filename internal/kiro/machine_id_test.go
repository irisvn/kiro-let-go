package kiro

import "testing"

func TestGenerateDeterministic(t *testing.T) {
	seed := "account-123"
	got1 := Generate(seed)
	got2 := Generate(seed)

	if got1 != got2 {
		t.Fatalf("expected deterministic output, got %q and %q", got1, got2)
	}
	if len(got1) != 64 {
		t.Fatalf("expected 64-char id, got %d", len(got1))
	}
	if err := Validate(got1); err != nil {
		t.Fatalf("expected generated id to validate, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "valid", id: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{name: "short", id: "abc", wantErr: true},
		{name: "uppercase", id: "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef", wantErr: true},
		{name: "nonhex", id: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.id)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.id)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}
