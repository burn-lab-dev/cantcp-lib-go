package cantcp

import "testing"

func TestCRC8Sum(t *testing.T) {
	tests := []struct {
		name string
		poly byte
		in   []byte
		want byte
	}{
		{name: "smbus check value", poly: 0x07, in: []byte("123456789"), want: 0xF4},
		{name: "custom poly check value", poly: 0x1D, in: []byte("123456789"), want: 0x37},
		{name: "empty input", poly: 0x07, in: nil, want: 0x00},
		{name: "zero byte", poly: 0x07, in: []byte{0x00}, want: 0x00},
		{name: "one byte", poly: 0x07, in: []byte{0x01}, want: 0x07},
		{name: "zero poly yields zero", poly: 0x00, in: []byte("123456789"), want: 0x00},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCRC8(tt.poly)
			if got := c.sum(tt.in); got != tt.want {
				t.Fatalf("sum(%q) with poly %#02x = %#02x, want %#02x", tt.in, tt.poly, got, tt.want)
			}
			if got := c.sum(tt.in); got != tt.want {
				t.Fatalf("second sum(%q) = %#02x, want %#02x (table mutated?)", tt.in, got, tt.want)
			}
		})
	}
}

func TestCRC8Table(t *testing.T) {
	tests := []struct {
		name string
		poly byte
		idx  int
		want byte
	}{
		{name: "poly 07 index 0", poly: 0x07, idx: 0, want: 0x00},
		{name: "poly 07 index 1", poly: 0x07, idx: 1, want: 0x07},
		{name: "poly 07 index 2", poly: 0x07, idx: 2, want: 0x0E},
		{name: "poly 07 index 3", poly: 0x07, idx: 3, want: 0x09},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCRC8(tt.poly)
			if got := c.tab[tt.idx]; got != tt.want {
				t.Fatalf("tab[%d] = %#02x, want %#02x", tt.idx, got, tt.want)
			}
		})
	}
}

func TestCRC8TablesAreIndependent(t *testing.T) {
	tests := []struct {
		name   string
		polyA  byte
		polyB  byte
		wantEq bool
	}{
		{name: "different polynomials", polyA: 0x07, polyB: 0x1D, wantEq: false},
		{name: "same polynomial", polyA: 0x07, polyB: 0x07, wantEq: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newCRC8(tt.polyA)
			b := newCRC8(tt.polyB)
			if got := a.tab == b.tab; got != tt.wantEq {
				t.Fatalf("tables equal = %v, want %v", got, tt.wantEq)
			}
		})
	}
}
