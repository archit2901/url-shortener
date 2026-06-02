package shortener

import "testing"

func TestEncode(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{61, "Z"},
		{62, "10"},
		{12345, "3d7"},
	}

	for _, tt := range tests {
		got := Encode(tt.input)
		if got != tt.expected {
			t.Errorf("Encode(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"0", 0},
		{"1", 1},
		{"Z", 61},
		{"10", 62},
		{"3d7", 12345},
	}

	for _, tt := range tests {
		got, err := Decode(tt.input)
		if err != nil {
			t.Errorf("Decode(%q) returned error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("Decode(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	values := []uint64{1, 100, 12345, 999999, 3521614606207}
	for _, v := range values {
		encoded := Encode(v)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Errorf("round trip failed for %d: %v", v, err)
			continue
		}
		if decoded != v {
			t.Errorf("round trip: %d -> %q -> %d", v, encoded, decoded)
		}
	}
}
