package smc

import (
	"testing"
)

func TestEncodeKey_Tp09(t *testing.T) {
	// "Tp09" as big-endian uint32:
	// 'T'=0x54, 'p'=0x70, '0'=0x30, '9'=0x39
	// Expected: 0x54703039
	got := encodeKey("Tp09")
	want := uint32(0x54703039)
	if got != want {
		t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", "Tp09", got, want)
	}
}

func TestEncodeKey_Tg0f(t *testing.T) {
	// "Tg0f" as big-endian uint32:
	// 'T'=0x54, 'g'=0x67, '0'=0x30, 'f'=0x66
	// Expected: 0x54673066
	got := encodeKey("Tg0f")
	want := uint32(0x54673066)
	if got != want {
		t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", "Tg0f", got, want)
	}
}

func TestEncodeKey_BigEndianOrdering(t *testing.T) {
	// Verify that the first character occupies the most significant byte.
	key := "ABCD"
	got := encodeKey(key)
	want := uint32('A')<<24 | uint32('B')<<16 | uint32('C')<<8 | uint32('D')

	if got != want {
		t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", key, got, want)
	}

	// Most significant byte should be 'A' (0x41).
	if (got >> 24) != 0x41 {
		t.Errorf("most significant byte = 0x%02X, want 0x41 ('A')", got>>24)
	}
}

func TestEncodeKey_MatchesCDefinition(t *testing.T) {
	// Verify the Go encodeKey matches the expected values from the C
	// smcKeyEncode function for known SMC type codes.
	tests := []struct {
		key  string
		want uint32
	}{
		{"flt ", 0x666c7420}, // SMC_TYPE_FLT
		{"sp78", 0x73703738}, // SMC_TYPE_SP78
		{"Tp0T", 0x54703054},
		{"Tp0P", 0x54703050},
		{"Tm0P", 0x546d3050},
		{"Ts0P", 0x54733050},
	}

	for _, tc := range tests {
		got := encodeKey(tc.key)
		if got != tc.want {
			t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", tc.key, got, tc.want)
		}
	}
}

func TestEncodeKey_AllSensorKeys(t *testing.T) {
	// Verify encodeKey does not panic for every defined sensor key.
	for _, s := range AppleSiliconSensors {
		if len(s.Key) != 4 {
			t.Errorf("sensor key %q (%s) is not 4 characters", s.Key, s.Name)
			continue
		}
		got := encodeKey(s.Key)
		if got == 0 {
			t.Errorf("encodeKey(%q) returned 0, which is unexpected for a valid key", s.Key)
		}
	}
}

func TestEncodeKey_AllSensorKeysExpectedValues(t *testing.T) {
	// Verify encodeKey produces the correct big-endian uint32 for every sensor key.
	for _, s := range AppleSiliconSensors {
		if len(s.Key) != 4 {
			continue
		}
		got := encodeKey(s.Key)
		want := uint32(s.Key[0])<<24 | uint32(s.Key[1])<<16 | uint32(s.Key[2])<<8 | uint32(s.Key[3])
		if got != want {
			t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", s.Key, got, want)
		}
	}
}

func TestAppleSiliconSensors_NoDuplicateKeys(t *testing.T) {
	seen := make(map[string]string) // key -> name
	for _, s := range AppleSiliconSensors {
		if prev, exists := seen[s.Key]; exists {
			t.Errorf("duplicate SMC key %q: used by both %q and %q", s.Key, prev, s.Name)
		}
		seen[s.Key] = s.Name
	}
}

func TestAppleSiliconSensors_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]string) // name -> key
	for _, s := range AppleSiliconSensors {
		if prev, exists := seen[s.Name]; exists {
			t.Errorf("duplicate sensor name %q: used by keys %q and %q", s.Name, prev, s.Key)
		}
		seen[s.Name] = s.Key
	}
}

func TestAppleSiliconSensors_AllKeysStartWithT(t *testing.T) {
	// Temperature sensor keys conventionally start with 'T'.
	for _, s := range AppleSiliconSensors {
		if len(s.Key) != 4 {
			continue
		}
		if s.Key[0] != 'T' {
			t.Errorf("sensor %q has key %q which does not start with 'T'", s.Name, s.Key)
		}
	}
}

func TestEncodeKey_SpecificSensorKeys(t *testing.T) {
	// Spot-check specific known sensor keys.
	tests := []struct {
		key  string
		want uint32
	}{
		{"Tp0T", 0x54703054},
		{"Tc0c", 0x54633063},
		{"TC0c", 0x54433063},
		{"Tg0f", 0x54673066},
		{"Tm0P", 0x546d3050},
		{"Ts0P", 0x54733050},
		{"TaLP", 0x5461_4c50},
		{"TaRP", 0x5461_5250},
	}
	for _, tc := range tests {
		got := encodeKey(tc.key)
		if got != tc.want {
			t.Errorf("encodeKey(%q) = 0x%08X, want 0x%08X", tc.key, got, tc.want)
		}
	}
}
