package luhn

import "testing"

func TestValid(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{name: "valid", number: "12345678903", want: true},
		{name: "valid short", number: "0", want: true},
		{name: "invalid checksum", number: "12345678904", want: false},
		{name: "letters", number: "123abc", want: false},
		{name: "empty", number: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.number); got != tt.want {
				t.Fatalf("Valid(%q) = %v, want %v", tt.number, got, tt.want)
			}
		})
	}
}
