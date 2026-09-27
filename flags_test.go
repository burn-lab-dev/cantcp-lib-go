package cantcp

import "testing"

func TestFlagConstants(t *testing.T) {
	tests := []struct {
		name string
		got  uint8
		want uint8
	}{
		{name: "FlagEFF", got: FlagEFF, want: 0x01},
		{name: "FlagRTR", got: FlagRTR, want: 0x02},
		{name: "FlagERR", got: FlagERR, want: 0x04},
		{name: "FlagBRS", got: FlagBRS, want: 0x08},
		{name: "FlagESI", got: FlagESI, want: 0x10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %#02x, want %#02x", tt.name, tt.got, tt.want)
			}
		})
	}
}
