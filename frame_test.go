package cantcp

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

// rawClassic builds a classic can_frame from a raw can_id word, can_dlc and
// payload.
func rawClassic(id uint32, dlc byte, data ...byte) []byte {
	b := make([]byte, frameLen)
	binary.LittleEndian.PutUint32(b[0:4], id)
	b[dlcOff] = dlc
	copy(b[dataOff:], data)
	return b
}

// rawFd builds a canfd_frame from a raw can_id word, len, flags byte and
// payload.
func rawFd(id uint32, length, flags byte, data ...byte) []byte {
	b := make([]byte, fdFrameLen)
	binary.LittleEndian.PutUint32(b[0:4], id)
	b[dlcOff] = length
	b[fdFlagsOff] = flags
	copy(b[dataOff:], data)
	return b
}

// testPayload returns n sequential bytes.
func testPayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// checkFrame verifies all exported fields of got against want.
func checkFrame(t *testing.T, got, want Frame) {
	t.Helper()
	if got.ID != want.ID {
		t.Fatalf("ID = %#x, want %#x", got.ID, want.ID)
	}
	if got.Type != want.Type {
		t.Fatalf("Type = %v, want %v", got.Type, want.Type)
	}
	if got.EFF != want.EFF || got.RTR != want.RTR || got.ERR != want.ERR ||
		got.BRS != want.BRS || got.ESI != want.ESI {
		t.Fatalf("flags EFF/RTR/ERR/BRS/ESI = %v/%v/%v/%v/%v, want %v/%v/%v/%v/%v",
			got.EFF, got.RTR, got.ERR, got.BRS, got.ESI,
			want.EFF, want.RTR, want.ERR, want.BRS, want.ESI)
	}
	if !slices.Equal(got.Data, want.Data) {
		t.Fatalf("Data = %x, want %x", got.Data, want.Data)
	}
}

func TestTypeString(t *testing.T) {
	tests := []struct {
		name string
		in   Type
		want string
	}{
		{name: "classic", in: TypeClassic, want: "CAN"},
		{name: "can fd", in: TypeFd, want: "CAN FD"},
		{name: "zero value", in: 0, want: "unknown"},
		{name: "unknown value", in: 0x7F, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Fatalf("Type(%#02x).String() = %q, want %q", byte(tt.in), got, tt.want)
			}
		})
	}
}

func TestFrameUnmarshalBinary(t *testing.T) {
	classicPad := rawClassic(0x1, 0)
	classicPad[fdFlagsOff] = 1
	classicRes0 := rawClassic(0x1, 0)
	classicRes0[fdFlagsOff+1] = 1
	classicRes1 := rawClassic(0x1, 0)
	classicRes1[fdFlagsOff+2] = 1
	fdRes0 := rawFd(0x1, 0, 0)
	fdRes0[6] = 1
	fdRes1 := rawFd(0x1, 0, 0)
	fdRes1[7] = 1

	tests := []struct {
		name    string
		pre     func(f *Frame)
		in      []byte
		wantErr error
		want    Frame
	}{
		{
			name: "classic sff with data",
			in:   rawClassic(0x123, 2, 0xDE, 0xAD),
			want: Frame{ID: 0x123, Type: TypeClassic, Data: []byte{0xDE, 0xAD}},
		},
		{
			name: "classic max sff id and dlc",
			in:   rawClassic(canSFFMask, maxDLC, testPayload(maxDLC)...),
			want: Frame{ID: canSFFMask, Type: TypeClassic, Data: testPayload(maxDLC)},
		},
		{
			name: "classic eff rtr",
			in:   rawClassic(canEFFFlag|canRTRFlag|0x1ABCDE, 0),
			want: Frame{ID: 0x1ABCDE, Type: TypeClassic, EFF: true, RTR: true},
		},
		{
			name: "classic err",
			in:   rawClassic(canERRFlag|0x123, 0),
			want: Frame{ID: 0x123, Type: TypeClassic, ERR: true},
		},
		{
			name: "classic err 29 bit class",
			in:   rawClassic(canERRFlag|canEFFMask, 0),
			want: Frame{ID: canEFFMask, Type: TypeClassic, ERR: true},
		},
		{
			name: "classic clears stale can fd flags",
			pre:  func(f *Frame) { f.BRS, f.ESI = true, true },
			in:   rawClassic(0x1, 0),
			want: Frame{ID: 0x1, Type: TypeClassic},
		},
		{
			name: "can fd brs esi",
			in:   rawFd(canEFFFlag|0x1ABCDE, 4, canFDBRS|canFDESI, 1, 2, 3, 4),
			want: Frame{ID: 0x1ABCDE, Type: TypeFd, EFF: true, BRS: true, ESI: true, Data: []byte{1, 2, 3, 4}},
		},
		{
			name: "can fd err",
			in:   rawFd(canERRFlag|0x123, 0, 0),
			want: Frame{ID: 0x123, Type: TypeFd, ERR: true},
		},
		{
			name: "can fd err 29 bit class",
			in:   rawFd(canERRFlag|0x1ABCDE, 0, 0),
			want: Frame{ID: 0x1ABCDE, Type: TypeFd, ERR: true},
		},
		{
			name: "can fd max len",
			in:   rawFd(0x1, maxFDDataLen, 0, testPayload(maxFDDataLen)...),
			want: Frame{ID: 0x1, Type: TypeFd, Data: testPayload(maxFDDataLen)},
		},
		{
			name: "can fd discrete len",
			in:   rawFd(0x1, 12, 0, testPayload(12)...),
			want: Frame{ID: 0x1, Type: TypeFd, Data: testPayload(12)},
		},
		{
			name: "can fd clears stale flags",
			pre:  func(f *Frame) { f.EFF, f.RTR, f.ERR, f.BRS, f.ESI = true, true, true, true, true },
			in:   rawFd(0x1, 0, 0),
			want: Frame{ID: 0x1, Type: TypeFd},
		},
		{name: "nil input", in: nil, wantErr: ErrFrameLen},
		{name: "short classic", in: rawClassic(0x1, 0)[:frameLen-1], wantErr: ErrFrameLen},
		{name: "long classic", in: append(rawClassic(0x1, 0), 0), wantErr: ErrFrameLen},
		{name: "short can fd", in: rawFd(0x1, 0, 0)[:fdFrameLen-1], wantErr: ErrFrameLen},
		{name: "long can fd", in: append(rawFd(0x1, 0, 0), 0), wantErr: ErrFrameLen},
		{name: "classic dlc too big", in: rawClassic(0x1, maxDLC+1), wantErr: ErrBadDLC},
		{name: "classic reserved pad", in: classicPad, wantErr: ErrReserved},
		{name: "classic reserved res0", in: classicRes0, wantErr: ErrReserved},
		{name: "classic reserved res1", in: classicRes1, wantErr: ErrReserved},
		{name: "classic sff id too big", in: rawClassic(canSFFMask+1, 0), wantErr: ErrBadID},
		{name: "can fd len too big", in: rawFd(0x1, maxFDDataLen+1, 0), wantErr: ErrBadLen},
		{name: "can fd len not encodable 9", in: rawFd(0x1, 9, 0), wantErr: ErrBadLen},
		{name: "can fd len not encodable 63", in: rawFd(0x1, 63, 0), wantErr: ErrBadLen},
		{name: "can fd fdf bit", in: rawFd(0x1, 0, 0x04), wantErr: ErrBadFlags},
		{name: "can fd unknown flag bit", in: rawFd(0x1, 0, 0x80), wantErr: ErrBadFlags},
		{name: "can fd reserved res0", in: fdRes0, wantErr: ErrReserved},
		{name: "can fd reserved res1", in: fdRes1, wantErr: ErrReserved},
		{name: "can fd rtr", in: rawFd(canRTRFlag|0x1, 0, 0), wantErr: ErrBadFlags},
		{name: "can fd sff id too big", in: rawFd(canSFFMask+1, 0, 0), wantErr: ErrBadID},
		{name: "classic err with eff", in: rawClassic(canERRFlag|canEFFFlag|0x123, 0), wantErr: ErrBadFlags},
		{name: "classic err with rtr", in: rawClassic(canERRFlag|canRTRFlag|0x123, 0), wantErr: ErrBadFlags},
		{name: "can fd err with eff", in: rawFd(canERRFlag|canEFFFlag|0x123, 0, 0), wantErr: ErrBadFlags},
		{name: "can fd err with rtr", in: rawFd(canERRFlag|canRTRFlag|0x123, 0, 0), wantErr: ErrBadFlags},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f Frame
			if tt.pre != nil {
				tt.pre(&f)
			}
			err := f.UnmarshalBinary(tt.in)
			checkError(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			checkFrame(t, f, tt.want)
			if got := f.GetRaw(); !bytes.Equal(got, tt.in) {
				t.Fatalf("GetRaw = %x, want %x", got, tt.in)
			}
			if len(f.Data) > 0 && &f.Data[0] != &f.raw[dataOff] {
				t.Fatal("Data is not a window into the raw frame")
			}
		})
	}
}

func TestFrameMarshalBinary(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr error
		want    []byte
	}{
		{
			name:  "classic minimal",
			frame: Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}},
			want:  rawClassic(0x123, 2, 0xDE, 0xAD),
		},
		{
			name:  "classic empty",
			frame: Frame{Type: TypeClassic},
			want:  rawClassic(0, 0),
		},
		{
			name:  "classic all flags and max dlc",
			frame: Frame{Type: TypeClassic, ID: 0x1ABCDE, EFF: true, RTR: true, Data: testPayload(maxDLC)},
			want:  rawClassic(canEFFFlag|canRTRFlag|0x1ABCDE, maxDLC, testPayload(maxDLC)...),
		},
		{
			name:  "classic error frame",
			frame: Frame{Type: TypeClassic, ID: 0x123, ERR: true, Data: []byte{0xAA}},
			want:  rawClassic(canERRFlag|0x123, 1, 0xAA),
		},
		{
			name:  "classic error frame 29 bit class",
			frame: Frame{Type: TypeClassic, ID: canEFFMask, ERR: true},
			want:  rawClassic(canERRFlag|canEFFMask, 0),
		},
		{
			name:  "classic max sff id",
			frame: Frame{Type: TypeClassic, ID: canSFFMask},
			want:  rawClassic(canSFFMask, 0),
		},
		{
			name:  "can fd with flags",
			frame: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, ESI: true, Data: []byte{1, 2}},
			want:  rawFd(canEFFFlag|0x1ABCDE, 2, canFDBRS|canFDESI, 1, 2),
		},
		{
			name:  "can fd error frame",
			frame: Frame{Type: TypeFd, ID: 0x1ABCDE, ERR: true, BRS: true, Data: []byte{1, 2}},
			want:  rawFd(canERRFlag|0x1ABCDE, 2, canFDBRS, 1, 2),
		},
		{
			name:  "can fd empty",
			frame: Frame{Type: TypeFd, ID: 0x123},
			want:  rawFd(0x123, 0, 0),
		},
		{
			name:  "can fd max len",
			frame: Frame{Type: TypeFd, ID: 0x123, Data: testPayload(maxFDDataLen)},
			want:  rawFd(0x123, maxFDDataLen, 0, testPayload(maxFDDataLen)...),
		},
		{
			name:  "can fd discrete len",
			frame: Frame{Type: TypeFd, ID: 0x123, Data: testPayload(12)},
			want:  rawFd(0x123, 12, 0, testPayload(12)...),
		},
		{name: "type not set", frame: Frame{ID: 0x1}, wantErr: ErrBadType},
		{name: "type unknown", frame: Frame{Type: 0x7F, ID: 0x1}, wantErr: ErrBadType},
		{name: "sff id too big", frame: Frame{Type: TypeClassic, ID: canSFFMask + 1}, wantErr: ErrBadID},
		{name: "eff id too big", frame: Frame{Type: TypeClassic, ID: canEFFMask + 1, EFF: true}, wantErr: ErrBadID},
		{name: "classic data too long", frame: Frame{Type: TypeClassic, Data: make([]byte, maxDLC+1)}, wantErr: ErrBadDLC},
		{name: "classic brs invalid", frame: Frame{Type: TypeClassic, BRS: true}, wantErr: ErrBadFlags},
		{name: "classic esi invalid", frame: Frame{Type: TypeClassic, ESI: true}, wantErr: ErrBadFlags},
		{name: "can fd data too long", frame: Frame{Type: TypeFd, Data: make([]byte, maxFDDataLen+1)}, wantErr: ErrBadLen},
		{name: "can fd data len not encodable", frame: Frame{Type: TypeFd, Data: make([]byte, 9)}, wantErr: ErrBadLen},
		{name: "can fd rtr invalid", frame: Frame{Type: TypeFd, RTR: true}, wantErr: ErrBadFlags},
		{name: "classic err with eff invalid", frame: Frame{Type: TypeClassic, ID: 0x123, ERR: true, EFF: true}, wantErr: ErrBadFlags},
		{name: "classic err with rtr invalid", frame: Frame{Type: TypeClassic, ID: 0x123, ERR: true, RTR: true}, wantErr: ErrBadFlags},
		{name: "can fd err with eff invalid", frame: Frame{Type: TypeFd, ID: 0x123, ERR: true, EFF: true}, wantErr: ErrBadFlags},
		{name: "classic err id too big", frame: Frame{Type: TypeClassic, ID: canEFFMask + 1, ERR: true}, wantErr: ErrBadID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.frame
			before := f.GetRaw()
			out, err := f.MarshalBinary()
			checkError(t, err, tt.wantErr)
			if tt.wantErr != nil {
				if out != nil {
					t.Fatalf("out = %x, want nil", out)
				}
				if !bytes.Equal(f.GetRaw(), before) {
					t.Fatalf("raw changed on error: got %x, want %x", f.GetRaw(), before)
				}
				return
			}
			if !bytes.Equal(out, tt.want) {
				t.Fatalf("raw = %x, want %x", out, tt.want)
			}
			if !bytes.Equal(f.GetRaw(), tt.want) {
				t.Fatalf("GetRaw = %x, want %x", f.GetRaw(), tt.want)
			}
			if !slices.Equal(f.Data, tt.frame.Data) {
				t.Fatalf("Data = %x, want %x", f.Data, tt.frame.Data)
			}
			if len(f.Data) > 0 && &f.Data[0] != &f.raw[dataOff] {
				t.Fatal("Data is not a window into the raw frame")
			}
			out[0] ^= 0xFF
			if f.raw[0] == out[0] {
				t.Fatal("MarshalBinary returned a slice into the internal buffer")
			}
		})
	}
}

func TestFrameMarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{name: "classic empty", frame: Frame{Type: TypeClassic}},
		{
			name:  "classic full",
			frame: Frame{Type: TypeClassic, ID: canSFFMask, EFF: true, RTR: true, Data: testPayload(maxDLC)},
		},
		{name: "classic error", frame: Frame{Type: TypeClassic, ID: canEFFMask, ERR: true, Data: testPayload(maxDLC)}},
		{name: "can fd empty", frame: Frame{Type: TypeFd}},
		{
			name:  "can fd full",
			frame: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, ESI: true, Data: testPayload(maxFDDataLen)},
		},
		{
			name:  "can fd error",
			frame: Frame{Type: TypeFd, ID: 0x1ABCDE, ERR: true, BRS: true, ESI: true, Data: testPayload(maxFDDataLen)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.frame
			raw, err := src.MarshalBinary()
			checkError(t, err, nil)
			var got Frame
			checkError(t, got.UnmarshalBinary(raw), nil)
			want := Frame{
				ID:   src.ID,
				Type: src.Type,
				EFF:  src.EFF,
				RTR:  src.RTR,
				ERR:  src.ERR,
				BRS:  src.BRS,
				ESI:  src.ESI,
				Data: src.Data,
			}
			checkFrame(t, got, want)
			if gotRaw := got.GetRaw(); !bytes.Equal(gotRaw, raw) {
				t.Fatalf("GetRaw = %x, want %x", gotRaw, raw)
			}
		})
	}
}

func TestFrameSetFlags(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		flags   uint8
		wantErr error
		want    Frame
	}{
		{
			name:  "classic eff rtr",
			frame: Frame{Type: TypeClassic},
			flags: FlagEFF | FlagRTR,
			want:  Frame{Type: TypeClassic, EFF: true, RTR: true},
		},
		{
			name:  "classic err",
			frame: Frame{Type: TypeClassic},
			flags: FlagERR,
			want:  Frame{Type: TypeClassic, ERR: true},
		},
		{
			name:    "classic err with eff invalid",
			frame:   Frame{Type: TypeClassic},
			flags:   FlagERR | FlagEFF,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeClassic},
		},
		{
			name:    "classic err with rtr invalid",
			frame:   Frame{Type: TypeClassic},
			flags:   FlagERR | FlagRTR,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeClassic},
		},
		{
			name:  "classic replaces previous flags",
			frame: Frame{Type: TypeClassic, EFF: true, RTR: true},
			flags: FlagEFF,
			want:  Frame{Type: TypeClassic, EFF: true},
		},
		{
			name:  "classic clears all flags",
			frame: Frame{Type: TypeClassic, EFF: true, RTR: true},
			flags: 0,
			want:  Frame{Type: TypeClassic},
		},
		{
			name:  "can fd brs esi",
			frame: Frame{Type: TypeFd},
			flags: FlagBRS | FlagESI,
			want:  Frame{Type: TypeFd, BRS: true, ESI: true},
		},
		{
			name:  "can fd err",
			frame: Frame{Type: TypeFd},
			flags: FlagERR,
			want:  Frame{Type: TypeFd, ERR: true},
		},
		{
			name:    "can fd err with eff invalid",
			frame:   Frame{Type: TypeFd},
			flags:   FlagEFF | FlagERR,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeFd},
		},
		{
			name:  "can fd replaces previous flags",
			frame: Frame{Type: TypeFd, EFF: true, BRS: true, ESI: true},
			flags: FlagEFF,
			want:  Frame{Type: TypeFd, EFF: true},
		},
		{
			name:    "classic brs invalid",
			frame:   Frame{Type: TypeClassic},
			flags:   FlagBRS,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeClassic},
		},
		{
			name:    "classic esi invalid",
			frame:   Frame{Type: TypeClassic},
			flags:   FlagESI,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeClassic},
		},
		{
			name:    "classic unknown bit",
			frame:   Frame{Type: TypeClassic},
			flags:   0x20,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeClassic},
		},
		{
			name:    "can fd rtr invalid",
			frame:   Frame{Type: TypeFd},
			flags:   FlagRTR,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeFd},
		},
		{
			name:    "can fd unknown bit",
			frame:   Frame{Type: TypeFd},
			flags:   0x80,
			wantErr: ErrBadFlags,
			want:    Frame{Type: TypeFd},
		},
		{
			name:    "type not set",
			frame:   Frame{},
			flags:   FlagEFF,
			wantErr: ErrBadType,
			want:    Frame{},
		},
		{
			name:    "type unknown",
			frame:   Frame{Type: 0x7F},
			flags:   FlagEFF,
			wantErr: ErrBadType,
			want:    Frame{Type: 0x7F},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.frame
			err := f.SetFlags(tt.flags)
			checkError(t, err, tt.wantErr)
			checkFrame(t, f, tt.want)
		})
	}
}

func TestFrameGetRaw(t *testing.T) {
	classicIn := rawClassic(0x123, 1, 0xAA)
	fdIn := rawFd(0x123, 1, 0, 0xBB)
	tests := []struct {
		name string
		pre  func(t *testing.T, f *Frame)
		want []byte
	}{
		{
			name: "classic",
			pre:  func(t *testing.T, f *Frame) { checkError(t, f.UnmarshalBinary(classicIn), nil) },
			want: classicIn,
		},
		{
			name: "can fd",
			pre:  func(t *testing.T, f *Frame) { checkError(t, f.UnmarshalBinary(fdIn), nil) },
			want: fdIn,
		},
		{
			name: "type not set",
			pre:  func(*testing.T, *Frame) {},
			want: nil,
		},
		{
			name: "type unknown",
			pre:  func(_ *testing.T, f *Frame) { f.Type = 0x7F },
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f Frame
			tt.pre(t, &f)
			got := f.GetRaw()
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("GetRaw = %x, want %x", got, tt.want)
			}
			if len(got) > 0 {
				got[0] ^= 0xFF
				if bytes.Equal(f.GetRaw(), got) {
					t.Fatal("GetRaw returned a slice into the internal buffer")
				}
			}
		})
	}
}

func TestFrameString(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
		want  string
	}{
		{
			name:  "classic all flags",
			frame: Frame{Type: TypeClassic, ID: 0x1ABCDE, EFF: true, RTR: true, Data: []byte{0xDE, 0xAD, 0xBE}},
			want:  "Frame{Type:CAN, ID:0x1abcde, Flags:EFF|RTR, DLC:3, Data:deadbe}",
		},
		{
			name:  "classic error",
			frame: Frame{Type: TypeClassic, ID: 0x123, ERR: true},
			want:  "Frame{Type:CAN, ID:0x123, Flags:ERR, DLC:0, Data:}",
		},
		{
			name:  "zero value",
			frame: Frame{},
			want:  "Frame{Type:unknown, ID:0x0, Flags:none, DLC:0, Data:}",
		},
		{
			name:  "can fd flags",
			frame: Frame{Type: TypeFd, ID: 0x123, EFF: true, BRS: true, ESI: true, Data: []byte{0x01, 0x02}},
			want:  "Frame{Type:CAN FD, ID:0x123, Flags:EFF|BRS|ESI, Len:2, Data:0102}",
		},
		{
			name:  "can fd no flags",
			frame: Frame{Type: TypeFd, ID: 0x1},
			want:  "Frame{Type:CAN FD, ID:0x1, Flags:none, Len:0, Data:}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.frame.String(); got != tt.want {
				t.Fatalf("String = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFrameValidFDDataLen(t *testing.T) {
	tests := []struct {
		name string
		n    byte
		want bool
	}{
		{name: "0", n: 0, want: true},
		{name: "1", n: 1, want: true},
		{name: "8", n: 8, want: true},
		{name: "9 not encodable", n: 9, want: false},
		{name: "11 not encodable", n: 11, want: false},
		{name: "12", n: 12, want: true},
		{name: "13 not encodable", n: 13, want: false},
		{name: "16", n: 16, want: true},
		{name: "20", n: 20, want: true},
		{name: "24", n: 24, want: true},
		{name: "32", n: 32, want: true},
		{name: "48", n: 48, want: true},
		{name: "63 not encodable", n: 63, want: false},
		{name: "64", n: 64, want: true},
		{name: "65 too big", n: 65, want: false},
		{name: "255 too big", n: 255, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validFDDataLen(tt.n); got != tt.want {
				t.Fatalf("validFDDataLen(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

func TestFrameUnmarshalBinaryKeepsStateOnError(t *testing.T) {
	good := rawClassic(canEFFFlag|0x1ABCDE, 2, 0xDE, 0xAD)
	reserved := rawClassic(0x1, 0)
	reserved[res1Off] = 1
	tests := []struct {
		name    string
		in      []byte
		wantErr error
	}{
		{name: "bad length", in: make([]byte, 17), wantErr: ErrFrameLen},
		{name: "bad id", in: rawClassic(canSFFMask+1, 0), wantErr: ErrBadID},
		{name: "bad flags", in: rawFd(canRTRFlag|0x1, 0, 0), wantErr: ErrBadFlags},
		{name: "bad len", in: rawFd(0x1, 9, 0), wantErr: ErrBadLen},
		{name: "reserved", in: reserved, wantErr: ErrReserved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f Frame
			checkError(t, f.UnmarshalBinary(good), nil)
			want := Frame{ID: 0x1ABCDE, Type: TypeClassic, EFF: true, Data: []byte{0xDE, 0xAD}}
			wantRaw := f.GetRaw()
			checkError(t, f.UnmarshalBinary(tt.in), tt.wantErr)
			checkFrame(t, f, want)
			if got := f.GetRaw(); !bytes.Equal(got, wantRaw) {
				t.Fatalf("raw changed on error: got %x, want %x", got, wantRaw)
			}
		})
	}
}
