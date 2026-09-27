package cantcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"testing"
)

// testFrame builds a classic can_frame with the given can_dlc and data.
func testFrame(dlc byte, data ...byte) []byte {
	f := make([]byte, frameLen)
	f[dlcOff] = dlc
	copy(f[dataOff:], data)
	return f
}

// testFrameFd builds a canfd_frame with the given len and data.
func testFrameFd(length byte, data ...byte) []byte {
	f := make([]byte, fdFrameLen)
	f[dlcOff] = length
	copy(f[dataOff:], data)
	return f
}

// packet wraps a raw frame into a valid packet using p's configuration,
// bypassing the Encode validation.
func packet(p *parser, frame []byte) []byte {
	var typ byte
	switch len(frame) {
	case frameLen:
		typ = byte(TypeClassic)
	case fdFrameLen:
		typ = byte(TypeFd)
	default:
		panic("packet: unsupported frame length")
	}
	pkt := make([]byte, 0, frameOff+len(frame)+crcLen)
	pkt = append(pkt, p.magic[0], p.magic[1], typ)
	pkt = append(pkt, frame...)
	pktLen := frameOff + len(frame) + crcLen
	return append(pkt, p.crc.sum(p.crcSpan(pkt, pktLen)))
}

// checkError verifies the type (errors.Is) and the text of an error.
func checkError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("err = %v, want %v", got, want)
	}
	if want == nil {
		return
	}
	if got == nil || got.Error() != want.Error() {
		t.Fatalf("error text = %v, want %q", got, want)
	}
}

func TestNewDefaults(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T, p *parser)
	}{
		{
			name: "magic",
			check: func(t *testing.T, p *parser) {
				if p.magic != ([magicLen]byte{0xC3, 0x3C}) {
					t.Fatalf("magic = %#v, want [0xC3 0x3C]", p.magic)
				}
			},
		},
		{
			name: "crc table",
			check: func(t *testing.T, p *parser) {
				if got := p.crc.sum([]byte("123456789")); got != 0xF4 {
					t.Fatalf("crc.sum = %#02x, want 0xF4 (poly 0x07)", got)
				}
			},
		},
		{
			name: "coverHeader",
			check: func(t *testing.T, p *parser) {
				if !p.coverHeader {
					t.Fatal("coverHeader = false, want true")
				}
			},
		},
		{
			name: "badPolicy",
			check: func(t *testing.T, p *parser) {
				if p.badPolicy != BadFrameSkip {
					t.Fatalf("badPolicy = %v, want BadFrameSkip", p.badPolicy)
				}
			},
		},
		{
			name: "logger",
			check: func(t *testing.T, p *parser) {
				if p.log != nil {
					t.Fatalf("logger = %v, want nil", p.log)
				}
			},
		},
		{
			name: "logLevel",
			check: func(t *testing.T, p *parser) {
				if p.logLevel != slog.LevelInfo {
					t.Fatalf("logLevel = %v, want Info", p.logLevel)
				}
			},
		},
		{
			name: "stats",
			check: func(t *testing.T, p *parser) {
				if got := p.Stats(); got != (Stats{}) {
					t.Fatalf("stats = %+v, want zero", got)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, New())
		})
	}
}

func TestOptions(t *testing.T) {
	logger := &captureLogger{}
	tests := []struct {
		name  string
		opts  []option
		check func(t *testing.T, p *parser)
	}{
		{
			name: "WithMagic replaces default",
			opts: []option{WithMagic(0x11, 0x22)},
			check: func(t *testing.T, p *parser) {
				if p.magic != ([magicLen]byte{0x11, 0x22}) {
					t.Fatalf("magic = %#v, want [0x11 0x22]", p.magic)
				}
			},
		},
		{
			name: "WithCRCPoly rebuilds table",
			opts: []option{WithCRCPoly(0x1D)},
			check: func(t *testing.T, p *parser) {
				if got := p.crc.sum([]byte("123456789")); got != 0x37 {
					t.Fatalf("crc.sum = %#02x, want 0x37 (poly 0x1D)", got)
				}
			},
		},
		{
			name: "WithCRCCoverFrameOnly",
			opts: []option{WithCRCCoverFrameOnly()},
			check: func(t *testing.T, p *parser) {
				if p.coverHeader {
					t.Fatal("coverHeader = true, want false")
				}
			},
		},
		{
			name: "WithBadFramePolicy",
			opts: []option{WithBadFramePolicy(BadFrameFail)},
			check: func(t *testing.T, p *parser) {
				if p.badPolicy != BadFrameFail {
					t.Fatalf("badPolicy = %v, want BadFrameFail", p.badPolicy)
				}
			},
		},
		{
			name: "WithLogger",
			opts: []option{WithLogger(logger)},
			check: func(t *testing.T, p *parser) {
				if p.log != logger {
					t.Fatal("logger was not set")
				}
			},
		},
		{
			name: "WithLogLevel",
			opts: []option{WithLogLevel(LevelTrace)},
			check: func(t *testing.T, p *parser) {
				if p.logLevel != LevelTrace {
					t.Fatalf("logLevel = %v, want LevelTrace", p.logLevel)
				}
			},
		},
		{
			name: "options apply in order",
			opts: []option{WithMagic(0x01, 0x02), WithMagic(0x03, 0x04)},
			check: func(t *testing.T, p *parser) {
				if p.magic != ([magicLen]byte{0x03, 0x04}) {
					t.Fatalf("magic = %#v, want [0x03 0x04]", p.magic)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, New(tt.opts...))
		})
	}
}

func TestSplit(t *testing.T) {
	build := New()
	classic := testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8)
	fd := testFrameFd(64, 0xAA, 0xBB)
	classicPkt := packet(build, classic)
	fdPkt := packet(build, fd)
	garbage := []byte{0x01, 0x02, 0x03}

	tests := []struct {
		name        string
		data        []byte
		atEOF       bool
		wantAdvance int
		wantToken   []byte
		wantErr     error
		wantStats   Stats
	}{
		{
			name:        "classic packet",
			data:        slices.Clone(classicPkt),
			wantAdvance: classicPktLen,
			wantToken:   classic,
		},
		{
			name:        "can fd packet",
			data:        slices.Clone(fdPkt),
			wantAdvance: fdPktLen,
			wantToken:   fd,
		},
		{
			name:        "classic then can fd",
			data:        append(slices.Clone(classicPkt), fdPkt...),
			wantAdvance: classicPktLen,
			wantToken:   classic,
		},
		{
			name:        "garbage before classic packet",
			data:        append(slices.Clone(garbage), classicPkt...),
			wantAdvance: len(garbage) + classicPktLen,
			wantToken:   classic,
			wantStats:   Stats{Skipped: len(garbage)},
		},
		{
			name:        "garbage before can fd packet",
			data:        append(slices.Clone(garbage), fdPkt...),
			wantAdvance: len(garbage) + fdPktLen,
			wantToken:   fd,
			wantStats:   Stats{Skipped: len(garbage)},
		},
		{
			name:        "garbage after classic packet",
			data:        append(slices.Clone(classicPkt), garbage...),
			wantAdvance: classicPktLen,
			wantToken:   classic,
		},
		{
			name:        "garbage without magic",
			data:        slices.Clone(garbage),
			wantAdvance: len(garbage),
			wantStats:   Stats{Skipped: len(garbage)},
		},
		{
			name:        "extra magic byte before packet",
			data:        append([]byte{0xC3}, classicPkt...),
			wantAdvance: 1 + classicPktLen,
			wantToken:   classic,
			wantStats:   Stats{Skipped: 1},
		},
		{
			name:        "second magic byte mismatch",
			data:        []byte{0xC3, 0x00},
			wantAdvance: 2,
			wantStats:   Stats{Skipped: 2},
		},
		{
			name:        "partial classic packet without eof",
			data:        slices.Clone(classicPkt[:5]),
			wantAdvance: 0,
		},
		{
			name:        "partial classic packet at eof",
			data:        slices.Clone(classicPkt[:5]),
			atEOF:       true,
			wantAdvance: 5,
			wantErr:     ErrTruncated,
			wantStats:   Stats{Truncated: 5},
		},
		{
			name:        "partial can fd packet without eof",
			data:        slices.Clone(fdPkt[:70]),
			wantAdvance: 0,
		},
		{
			name:        "partial can fd packet at eof",
			data:        slices.Clone(fdPkt[:70]),
			atEOF:       true,
			wantAdvance: 70,
			wantErr:     ErrTruncated,
			wantStats:   Stats{Truncated: 70},
		},
		{
			name:        "missing type byte without eof",
			data:        []byte{0xC3, 0x3C},
			wantAdvance: 0,
		},
		{
			name:        "missing type byte at eof",
			data:        []byte{0xC3, 0x3C},
			atEOF:       true,
			wantAdvance: 2,
			wantErr:     ErrTruncated,
			wantStats:   Stats{Truncated: 2},
		},
		{
			name:        "single magic byte at eof",
			data:        []byte{0xC3},
			atEOF:       true,
			wantAdvance: 1,
			wantErr:     ErrTruncated,
			wantStats:   Stats{Truncated: 1},
		},
		{
			name:        "empty input at eof",
			data:        nil,
			atEOF:       true,
			wantAdvance: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			advance, token, err := p.Split(tt.data, tt.atEOF)
			checkError(t, err, tt.wantErr)
			if advance != tt.wantAdvance {
				t.Fatalf("advance = %d, want %d", advance, tt.wantAdvance)
			}
			if !bytes.Equal(token, tt.wantToken) {
				t.Fatalf("token = %x, want %x", token, tt.wantToken)
			}
			if got := p.Stats(); got != tt.wantStats {
				t.Fatalf("stats = %+v, want %+v", got, tt.wantStats)
			}
		})
	}
}

func TestSplitResync(t *testing.T) {
	valid := testFrame(8, 0x11)
	fdValid := testFrameFd(64, 0x22)
	tests := []struct {
		name        string
		opts        []option
		data        func(t *testing.T, p *parser) []byte
		wantAdvance int
		wantToken   []byte
		wantErr     error
		wantStats   Stats
	}{
		{
			name: "crc mismatch then valid classic packet",
			data: func(t *testing.T, p *parser) []byte {
				bad := append([]byte{0xC3, 0x3C, byte(TypeClassic)}, make([]byte, frameLen+1)...)
				if p.crc.sum(bad[:classicPktLen-1]) == bad[classicPktLen-1] {
					t.Fatal("test precondition: broken candidate must fail CRC")
				}
				return append(bad, packet(p, valid)...)
			},
			wantAdvance: 2 * classicPktLen,
			wantToken:   valid,
			wantStats:   Stats{Skipped: classicPktLen, Dropped: 1},
		},
		{
			name: "crc mismatch then valid can fd packet",
			data: func(t *testing.T, p *parser) []byte {
				bad := append([]byte{0xC3, 0x3C, byte(TypeFd)}, make([]byte, fdFrameLen+1)...)
				if p.crc.sum(bad[:fdPktLen-1]) == bad[fdPktLen-1] {
					t.Fatal("test precondition: broken candidate must fail CRC")
				}
				return append(bad, packet(p, fdValid)...)
			},
			wantAdvance: 2 * fdPktLen,
			wantToken:   fdValid,
			wantStats:   Stats{Skipped: fdPktLen, Dropped: 1},
		},
		{
			name: "bad dlc skipped then valid packet",
			data: func(t *testing.T, p *parser) []byte {
				bad := packet(p, testFrame(9))
				if bad[classicPktLen-1] == p.magic[0] {
					t.Fatal("test precondition: CRC byte must not collide with magic")
				}
				return append(bad, packet(p, valid)...)
			},
			wantAdvance: 2 * classicPktLen,
			wantToken:   valid,
			wantStats:   Stats{Skipped: classicPktLen, BadDLC: 1},
		},
		{
			name: "bad dlc fails",
			opts: []option{WithBadFramePolicy(BadFrameFail)},
			data: func(t *testing.T, p *parser) []byte {
				return packet(p, testFrame(9))
			},
			wantAdvance: 0,
			wantErr:     ErrBadDLC,
			wantStats:   Stats{BadDLC: 1},
		},
		{
			name: "bad fd len skipped then valid packet",
			data: func(t *testing.T, p *parser) []byte {
				bad := packet(p, testFrameFd(65))
				if bad[fdPktLen-1] == p.magic[0] {
					t.Fatal("test precondition: CRC byte must not collide with magic")
				}
				return append(bad, packet(p, fdValid)...)
			},
			wantAdvance: 2 * fdPktLen,
			wantToken:   fdValid,
			wantStats:   Stats{Skipped: fdPktLen, BadLen: 1},
		},
		{
			name: "bad fd len fails",
			opts: []option{WithBadFramePolicy(BadFrameFail)},
			data: func(t *testing.T, p *parser) []byte {
				return packet(p, testFrameFd(65))
			},
			wantAdvance: 0,
			wantErr:     ErrBadLen,
			wantStats:   Stats{BadLen: 1},
		},
		{
			name: "unknown type skipped then valid packet",
			data: func(t *testing.T, p *parser) []byte {
				return append([]byte{p.magic[0], p.magic[1], 0x7F}, packet(p, valid)...)
			},
			wantAdvance: 3 + classicPktLen,
			wantToken:   valid,
			wantStats:   Stats{Skipped: 3, BadType: 1},
		},
		{
			name: "unknown type fails",
			opts: []option{WithBadFramePolicy(BadFrameFail)},
			data: func(t *testing.T, p *parser) []byte {
				return []byte{p.magic[0], p.magic[1], 0x7F}
			},
			wantAdvance: 0,
			wantErr:     ErrBadType,
			wantStats:   Stats{BadType: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts...)
			advance, token, err := p.Split(tt.data(t, p), false)
			checkError(t, err, tt.wantErr)
			if advance != tt.wantAdvance {
				t.Fatalf("advance = %d, want %d", advance, tt.wantAdvance)
			}
			if !bytes.Equal(token, tt.wantToken) {
				t.Fatalf("token = %x, want %x", token, tt.wantToken)
			}
			if got := p.Stats(); got != tt.wantStats {
				t.Fatalf("stats = %+v, want %+v", got, tt.wantStats)
			}
		})
	}
}

func TestEncode(t *testing.T) {
	tests := []struct {
		name      string
		opts      []option
		dst       []byte
		frame     []byte
		wantErr   error
		wantMagic []byte
		wantType  byte
	}{
		{
			name:      "classic frame",
			frame:     testFrame(8, 1, 2, 3),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeClassic),
		},
		{
			name:      "can fd frame",
			frame:     testFrameFd(64, 1, 2, 3),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeFd),
		},
		{
			name:      "zero dlc",
			frame:     testFrame(0),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeClassic),
		},
		{
			name:      "zero len can fd",
			frame:     testFrameFd(0),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeFd),
		},
		{
			name:      "max dlc",
			frame:     testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeClassic),
		},
		{
			name:      "custom magic",
			opts:      []option{WithMagic(0x11, 0x22)},
			frame:     testFrame(1, 0xAA),
			wantMagic: []byte{0x11, 0x22},
			wantType:  byte(TypeClassic),
		},
		{
			name:      "appends to dst",
			dst:       []byte{0xFF},
			frame:     testFrame(1, 0xAA),
			wantMagic: []byte{0xC3, 0x3C},
			wantType:  byte(TypeClassic),
		},
		{name: "short frame", frame: testFrame(8)[:15], wantErr: ErrFrameLen},
		{name: "nil frame", frame: nil, wantErr: ErrFrameLen},
		{name: "frame between sizes", frame: make([]byte, 17), wantErr: ErrFrameLen},
		{name: "frame longer than can fd", frame: make([]byte, 73), wantErr: ErrFrameLen},
		{name: "classic dlc too big", frame: testFrame(9), wantErr: ErrBadDLC},
		{name: "can fd len too big", frame: testFrameFd(65), wantErr: ErrBadLen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts...)
			before := slices.Clone(tt.dst)
			got, err := p.Encode(tt.dst, tt.frame)
			checkError(t, err, tt.wantErr)
			if tt.wantErr != nil {
				if !bytes.Equal(got, before) {
					t.Fatalf("dst changed on error: got %x, want %x", got, before)
				}
				return
			}
			wantLen := frameOff + len(tt.frame) + crcLen
			if len(got) != len(before)+wantLen {
				t.Fatalf("packet length = %d, want %d", len(got), len(before)+wantLen)
			}
			pkt := got[len(before):]
			if !bytes.Equal(pkt[:magicLen], tt.wantMagic) {
				t.Fatalf("magic = %x, want %x", pkt[:magicLen], tt.wantMagic)
			}
			if pkt[typeOff] != tt.wantType {
				t.Fatalf("type = %#02x, want %#02x", pkt[typeOff], tt.wantType)
			}
			advance, token, err := p.Split(pkt, true)
			checkError(t, err, nil)
			if advance != wantLen {
				t.Fatalf("advance = %d, want %d", advance, wantLen)
			}
			if !bytes.Equal(token, tt.frame) {
				t.Fatalf("token = %x, want %x", token, tt.frame)
			}
		})
	}
}

func TestEncodeSplitRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		opts  []option
		frame []byte
	}{
		{name: "classic default", frame: testFrame(8, 0xDE, 0xAD, 0xBE, 0xEF)},
		{name: "can fd default", frame: testFrameFd(64, 0xDE, 0xAD, 0xBE, 0xEF)},
		{name: "custom magic", opts: []option{WithMagic(0x11, 0x22)}, frame: testFrame(8, 1)},
		{name: "custom magic can fd", opts: []option{WithMagic(0x11, 0x22)}, frame: testFrameFd(8, 1)},
		{name: "custom poly", opts: []option{WithCRCPoly(0x1D)}, frame: testFrame(8, 2)},
		{name: "custom poly can fd", opts: []option{WithCRCPoly(0x1D)}, frame: testFrameFd(8, 2)},
		{name: "frame only coverage", opts: []option{WithCRCCoverFrameOnly()}, frame: testFrame(8, 3)},
		{name: "frame only coverage can fd", opts: []option{WithCRCCoverFrameOnly()}, frame: testFrameFd(8, 3)},
		{name: "zero dlc", frame: testFrame(0)},
		{name: "zero len can fd", frame: testFrameFd(0)},
		{name: "all bytes set", frame: testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8)},
		{name: "max len can fd", frame: testFrameFd(64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts...)
			pkt, err := p.Encode(nil, tt.frame)
			checkError(t, err, nil)
			advance, token, err := p.Split(pkt, true)
			checkError(t, err, nil)
			if advance != len(pkt) {
				t.Fatalf("advance = %d, want %d", advance, len(pkt))
			}
			if !bytes.Equal(token, tt.frame) {
				t.Fatalf("token = %x, want %x", token, tt.frame)
			}
			if got := p.Stats(); got != (Stats{}) {
				t.Fatalf("stats = %+v, want zero", got)
			}
		})
	}
}

func TestCRCCoverageMismatch(t *testing.T) {
	tests := []struct {
		name  string
		enc   *parser
		dec   *parser
		frame []byte
	}{
		{
			name:  "frame only encoded, header covered decoded",
			enc:   New(WithCRCCoverFrameOnly()),
			dec:   New(),
			frame: testFrame(8, 1),
		},
		{
			name:  "header covered encoded, frame only decoded",
			enc:   New(),
			dec:   New(WithCRCCoverFrameOnly()),
			frame: testFrame(8, 2),
		},
		{
			name:  "can fd frame only encoded, header covered decoded",
			enc:   New(WithCRCCoverFrameOnly()),
			dec:   New(),
			frame: testFrameFd(64, 3),
		},
		{
			name:  "can fd header covered encoded, frame only decoded",
			enc:   New(),
			dec:   New(WithCRCCoverFrameOnly()),
			frame: testFrameFd(64, 4),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt, err := tt.enc.Encode(nil, tt.frame)
			checkError(t, err, nil)
			advance, token, err := tt.dec.Split(pkt, false)
			checkError(t, err, nil)
			if token != nil {
				t.Fatalf("token = %x, want nil (CRC must not match)", token)
			}
			if advance != len(pkt) {
				t.Fatalf("advance = %d, want %d", advance, len(pkt))
			}
			if got := tt.dec.Stats(); got.Dropped != 1 {
				t.Fatalf("stats = %+v, want Dropped 1", got)
			}
		})
	}
}

func TestStatsReset(t *testing.T) {
	p := New()
	steps := []struct {
		name   string
		action func(p *parser)
		want   Stats
	}{
		{
			name:   "initial",
			action: func(*parser) {},
			want:   Stats{},
		},
		{
			name:   "garbage skipped",
			action: func(p *parser) { p.Split([]byte{0x01, 0x02}, false) },
			want:   Stats{Skipped: 2},
		},
		{
			name:   "bad type counted",
			action: func(p *parser) { p.Split([]byte{0xC3, 0x3C, 0x7F}, false) },
			want:   Stats{Skipped: 5, BadType: 1},
		},
		{
			name:   "reset",
			action: func(p *parser) { p.ResetStats() },
			want:   Stats{},
		},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			step.action(p)
			if got := p.Stats(); got != step.want {
				t.Fatalf("stats = %+v, want %+v", got, step.want)
			}
		})
	}
}

// TestStatsSaturation verifies that the stream counters saturate at MaxInt
// instead of overflowing: a garbage stream can drive them without bound.
func TestStatsSaturation(t *testing.T) {
	// badDLCPacket builds a well-formed classic packet with can_dlc 9; the
	// CRC is recomputed so the packet passes the CRC check and reaches the
	// length-field check.
	badDLCPacket := func(p *parser) []byte {
		pkt := packet(p, testFrame(0))
		pkt[frameOff+dlcOff] = maxDLC + 1
		pkt[len(pkt)-1] = p.crc.sum(p.crcSpan(pkt, len(pkt)))
		return pkt
	}
	// badLenPacket builds a well-formed CAN FD packet with len 65.
	badLenPacket := func(p *parser) []byte {
		pkt := packet(p, testFrameFd(0))
		pkt[frameOff+dlcOff] = maxFDDataLen + 1
		pkt[len(pkt)-1] = p.crc.sum(p.crcSpan(pkt, len(pkt)))
		return pkt
	}
	// corruptCRCPacket builds a structurally valid classic packet with a
	// broken CRC byte.
	corruptCRCPacket := func(p *parser) []byte {
		pkt := packet(p, testFrame(0))
		pkt[len(pkt)-1] ^= 0xFF
		return pkt
	}

	tests := []struct {
		name    string
		start   Stats
		action  func(p *parser) error
		want    Stats
		wantErr error
	}{
		{
			name:   "skipped grows",
			start:  Stats{Skipped: 5},
			action: func(p *parser) error { p.Split([]byte{0x01, 0x02}, false); return nil },
			want:   Stats{Skipped: 7},
		},
		{
			name:   "skipped saturates",
			start:  Stats{Skipped: math.MaxInt},
			action: func(p *parser) error { p.Split([]byte{0x01, 0x02}, false); return nil },
			want:   Stats{Skipped: math.MaxInt},
		},
		{
			name:   "skipped saturates at the boundary",
			start:  Stats{Skipped: math.MaxInt - 1},
			action: func(p *parser) error { p.Split([]byte{0x01, 0x02}, false); return nil },
			want:   Stats{Skipped: math.MaxInt},
		},
		{
			name:    "truncated saturates",
			start:   Stats{Truncated: math.MaxInt},
			action:  func(p *parser) error { _, _, err := p.Split([]byte{0xC3}, true); return err },
			want:    Stats{Truncated: math.MaxInt},
			wantErr: ErrTruncated,
		},
		{
			name:   "bad type saturates",
			start:  Stats{BadType: math.MaxInt},
			action: func(p *parser) error { p.Split([]byte{0xC3, 0x3C, 0x7F}, false); return nil },
			want:   Stats{BadType: math.MaxInt, Skipped: 3},
		},
		{
			name:   "bad dlc saturates",
			start:  Stats{BadDLC: math.MaxInt},
			action: func(p *parser) error { p.Split(badDLCPacket(p), false); return nil },
			want:   Stats{BadDLC: math.MaxInt, Skipped: classicPktLen},
		},
		{
			name:   "bad len saturates",
			start:  Stats{BadLen: math.MaxInt},
			action: func(p *parser) error { p.Split(badLenPacket(p), false); return nil },
			want:   Stats{BadLen: math.MaxInt, Skipped: fdPktLen},
		},
		{
			name:   "dropped saturates",
			start:  Stats{Dropped: math.MaxInt},
			action: func(p *parser) error { p.Split(corruptCRCPacket(p), false); return nil },
			want:   Stats{Dropped: math.MaxInt, Skipped: classicPktLen},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			p.stats = tt.start
			err := tt.action(p)
			checkError(t, err, tt.wantErr)
			if got := p.Stats(); got != tt.want {
				t.Fatalf("stats = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestScannerChunked(t *testing.T) {
	build := New()
	frame1 := testFrame(8, 1, 2, 3)
	frame2 := testFrameFd(64, 4, 5, 6)
	garbage := []byte{0x01, 0x02, 0x03}
	stream := append(slices.Clone(garbage), packet(build, frame1)...)
	stream = append(stream, packet(build, frame2)...)
	stream = append(stream, garbage...)

	tests := []struct {
		name      string
		chunkSize int
	}{
		{name: "byte by byte", chunkSize: 1},
		{name: "chunk 3", chunkSize: 3},
		{name: "chunk 7", chunkSize: 7},
		{name: "classic packet sized", chunkSize: classicPktLen},
		{name: "can fd packet sized", chunkSize: fdPktLen},
		{name: "whole stream", chunkSize: len(stream)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			sc := bufio.NewScanner(&chunkReader{data: slices.Clone(stream), size: tt.chunkSize})
			sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
			sc.Split(p.Split)
			var frames [][]byte
			for sc.Scan() {
				frames = append(frames, bytes.Clone(sc.Bytes()))
			}
			checkError(t, sc.Err(), nil)
			if len(frames) != 2 {
				t.Fatalf("got %d frames, want 2", len(frames))
			}
			if !bytes.Equal(frames[0], frame1) {
				t.Fatalf("frame 0 = %x, want %x", frames[0], frame1)
			}
			if !bytes.Equal(frames[1], frame2) {
				t.Fatalf("frame 1 = %x, want %x", frames[1], frame2)
			}
			if got := p.Stats(); got.Skipped != 2*len(garbage) {
				t.Fatalf("stats = %+v, want Skipped %d", got, 2*len(garbage))
			}
		})
	}
}

func TestNilLogger(t *testing.T) {
	tests := []struct {
		name   string
		action func(p *parser)
	}{
		{
			name:   "classic packet",
			action: func(p *parser) { p.Split(packet(p, testFrame(8, 1)), false) },
		},
		{
			name:   "can fd packet",
			action: func(p *parser) { p.Split(packet(p, testFrameFd(64, 1)), false) },
		},
		{
			name:   "garbage",
			action: func(p *parser) { p.Split([]byte{0x01, 0x02}, false) },
		},
		{
			name:   "truncated",
			action: func(p *parser) { p.Split([]byte{0xC3, 0x3C, 0x01}, true) },
		},
		{
			name:   "unknown type",
			action: func(p *parser) { p.Split([]byte{0xC3, 0x3C, 0x7F}, false) },
		},
		{
			name:   "bad dlc",
			action: func(p *parser) { p.Split(packet(p, testFrame(9)), false) },
		},
		{
			name:   "bad fd len",
			action: func(p *parser) { p.Split(packet(p, testFrameFd(65)), false) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			tt.action(p)
		})
	}
}

func TestLogger(t *testing.T) {
	classic := testFrame(8, 0x11)
	fd := testFrameFd(64, 0x22)
	tests := []struct {
		name     string
		opts     []option
		action   func(p *parser)
		wantLogs []logRecord
	}{
		{
			name:   "trace classic frame",
			opts:   []option{WithLogLevel(LevelTrace)},
			action: func(p *parser) { p.Split(packet(p, classic), false) },
			wantLogs: []logRecord{
				{level: LevelTrace, msg: "frame parsed", args: []any{
					"type", byte(TypeClassic),
					"length", byte(8),
					"frame", hex.EncodeToString(classic),
				}},
			},
		},
		{
			name:   "trace can fd frame",
			opts:   []option{WithLogLevel(LevelTrace)},
			action: func(p *parser) { p.Split(packet(p, fd), false) },
			wantLogs: []logRecord{
				{level: LevelTrace, msg: "frame parsed", args: []any{
					"type", byte(TypeFd),
					"length", byte(64),
					"frame", hex.EncodeToString(fd),
				}},
			},
		},
		{
			name:     "info level hides trace",
			opts:     []option{WithLogLevel(slog.LevelInfo)},
			action:   func(p *parser) { p.Split(packet(p, classic), false) },
			wantLogs: nil,
		},
		{
			name:   "garbage skipped",
			opts:   []option{WithLogLevel(LevelTrace)},
			action: func(p *parser) { p.Split([]byte{0x01, 0x02}, false) },
			wantLogs: []logRecord{
				{level: LevelTrace, msg: "garbage skipped", args: []any{"bytes", 2}},
			},
		},
		{
			name: "crc mismatch",
			opts: []option{WithLogLevel(LevelTrace)},
			action: func(p *parser) {
				bad := append([]byte{0xC3, 0x3C, byte(TypeClassic)}, make([]byte, frameLen+1)...)
				p.Split(bad, false)
			},
			wantLogs: []logRecord{
				{level: LevelTrace, msg: "crc mismatch", args: []any{"candidate", 1}},
				{level: LevelTrace, msg: "garbage skipped", args: []any{"bytes", classicPktLen - 1}},
			},
		},
		{
			name:   "unknown type warns",
			opts:   []option{WithLogLevel(slog.LevelInfo)},
			action: func(p *parser) { p.Split([]byte{0xC3, 0x3C, 0x7F}, false) },
			wantLogs: []logRecord{
				{level: slog.LevelWarn, msg: "unknown packet type", args: []any{"type", byte(0x7F)}},
			},
		},
		{
			name:   "bad dlc warns",
			opts:   []option{WithLogLevel(slog.LevelInfo)},
			action: func(p *parser) { p.Split(packet(p, testFrame(9)), false) },
			wantLogs: []logRecord{
				{level: slog.LevelWarn, msg: "can_dlc > 8", args: []any{"dlc", byte(9)}},
			},
		},
		{
			name:   "bad fd len warns",
			opts:   []option{WithLogLevel(slog.LevelInfo)},
			action: func(p *parser) { p.Split(packet(p, testFrameFd(65)), false) },
			wantLogs: []logRecord{
				{level: slog.LevelWarn, msg: "canfd len > 64", args: []any{"len", byte(65)}},
			},
		},
		{
			name:   "truncated warns",
			opts:   []option{WithLogLevel(slog.LevelInfo)},
			action: func(p *parser) { p.Split([]byte{0xC3, 0x3C, 0x01, 0x00}, true) },
			wantLogs: []logRecord{
				{level: slog.LevelWarn, msg: "stream truncated", args: []any{"bytes", 4}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &captureLogger{}
			opts := append(slices.Clone(tt.opts), WithLogger(logger))
			p := New(opts...)
			tt.action(p)
			if len(logger.records) != len(tt.wantLogs) {
				t.Fatalf("got %d log records %+v, want %d", len(logger.records), logger.records, len(tt.wantLogs))
			}
			for i, want := range tt.wantLogs {
				got := logger.records[i]
				if got.level != want.level || got.msg != want.msg {
					t.Errorf("record %d = %v/%q, want %v/%q", i, got.level, got.msg, want.level, want.msg)
				}
				if !slices.Equal(got.args, want.args) {
					t.Errorf("record %d args = %v, want %v", i, got.args, want.args)
				}
			}
		})
	}
}

func TestSplitEOFPrefixes(t *testing.T) {
	build := New()
	tests := []struct {
		name string
		pkt  []byte
	}{
		{name: "classic packet", pkt: packet(build, testFrame(8, 1, 2, 3))},
		{name: "can fd packet", pkt: packet(build, testFrameFd(64, 4, 5))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for n := range len(tt.pkt) {
				p := New()
				advance, token, err := p.Split(slices.Clone(tt.pkt[:n]), true)
				if n == 0 {
					checkError(t, err, nil)
					if advance != 0 || token != nil {
						t.Fatalf("prefix 0: advance = %d, token = %x, want 0, nil", advance, token)
					}
					continue
				}
				checkError(t, err, ErrTruncated)
				if advance != n {
					t.Fatalf("prefix %d: advance = %d, want %d", n, advance, n)
				}
				if token != nil {
					t.Fatalf("prefix %d: token = %x, want nil", n, token)
				}
				if got := p.Stats(); got.Truncated != n || got.Skipped != 0 || got.Dropped != 0 {
					t.Fatalf("prefix %d: stats = %+v, want Truncated %d only", n, got, n)
				}
			}
			p := New()
			advance, token, err := p.Split(slices.Clone(tt.pkt), true)
			checkError(t, err, nil)
			if advance != len(tt.pkt) {
				t.Fatalf("full packet: advance = %d, want %d", advance, len(tt.pkt))
			}
			if len(token) == 0 {
				t.Fatal("full packet: no token")
			}
		})
	}
}

func TestSplitTypeSwap(t *testing.T) {
	classic := packet(New(), testFrame(8, 1, 2, 3))
	fd := packet(New(), testFrameFd(64, 4, 5))
	asFD := slices.Clone(classic)
	asFD[typeOff] = byte(TypeFd)
	asClassic := slices.Clone(fd)
	asClassic[typeOff] = byte(TypeClassic)

	tests := []struct {
		name        string
		opts        []option
		data        []byte
		wantAdvance int
		wantErr     error
		wantDropped int
	}{
		{
			name:        "classic packet as can fd",
			data:        asFD,
			wantAdvance: len(asFD),
			wantErr:     ErrTruncated,
		},
		{
			name:        "classic packet as can fd, header not covered",
			opts:        []option{WithCRCCoverFrameOnly()},
			data:        asFD,
			wantAdvance: len(asFD),
			wantErr:     ErrTruncated,
		},
		{
			name:        "can fd packet as classic",
			data:        asClassic,
			wantAdvance: len(asClassic),
			wantDropped: 1,
		},
		{
			name:        "can fd packet as classic, header not covered",
			opts:        []option{WithCRCCoverFrameOnly()},
			data:        asClassic,
			wantAdvance: len(asClassic),
			wantDropped: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts...)
			advance, token, err := p.Split(tt.data, true)
			checkError(t, err, tt.wantErr)
			if token != nil {
				t.Fatalf("token = %x, want nil", token)
			}
			if advance != tt.wantAdvance {
				t.Fatalf("advance = %d, want %d", advance, tt.wantAdvance)
			}
			if got := p.Stats(); got.Dropped != tt.wantDropped {
				t.Fatalf("stats = %+v, want Dropped %d", got, tt.wantDropped)
			}
		})
	}
}

func TestStatsConservation(t *testing.T) {
	build := New()
	classic := packet(build, testFrame(8, 1, 2, 3))
	fd := packet(build, testFrameFd(64, 4, 5))
	badType := []byte{0xC3, 0x3C, 0x7F, 0x11}
	broken := append([]byte{0xC3, 0x3C, byte(TypeClassic)}, make([]byte, frameLen+1)...)
	garbage1 := []byte{0x01, 0x02}
	garbage2 := []byte{0x03, 0x04, 0x05}
	stream := slices.Concat(garbage1, classic, badType, broken, fd, garbage2)

	tests := []struct {
		name string
	}{
		{name: "dirty stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			sc := bufio.NewScanner(bytes.NewReader(stream))
			sc.Split(p.Split)
			var packets int
			for sc.Scan() {
				packets += len(sc.Bytes()) + frameOff + crcLen
			}
			checkError(t, sc.Err(), nil)
			got := p.Stats()
			if got.Truncated != 0 {
				t.Fatalf("stats = %+v, want Truncated 0", got)
			}
			if want := len(stream) - packets; got.Skipped != want {
				t.Fatalf("Skipped = %d, want %d (stream %d, packets %d)", got.Skipped, want, len(stream), packets)
			}
			if got.BadType != 1 || got.Dropped != 1 {
				t.Fatalf("stats = %+v, want BadType 1, Dropped 1", got)
			}
		})
	}
}

func TestScannerFragmentation(t *testing.T) {
	build := New()
	frame1 := testFrame(8, 1, 2, 3)
	frame2 := testFrameFd(64, 4, 5, 6)
	garbage := []byte{0x01, 0x02, 0x03}
	stream := slices.Concat(garbage, packet(build, frame1), packet(build, frame2), garbage)

	tests := []struct {
		name string
		seed int64
		max  int
	}{
		{name: "seed 1", seed: 1, max: 5},
		{name: "seed 7", seed: 7, max: 2},
		{name: "seed 42", seed: 42, max: 17},
		{name: "seed 12345", seed: 12345, max: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			r := &randChunkReader{
				data: slices.Clone(stream),
				rnd:  rand.New(rand.NewSource(tt.seed)),
				max:  tt.max,
			}
			sc := bufio.NewScanner(r)
			sc.Split(p.Split)
			var frames [][]byte
			for sc.Scan() {
				frames = append(frames, bytes.Clone(sc.Bytes()))
			}
			checkError(t, sc.Err(), nil)
			if len(frames) != 2 {
				t.Fatalf("got %d frames, want 2", len(frames))
			}
			if !bytes.Equal(frames[0], frame1) || !bytes.Equal(frames[1], frame2) {
				t.Fatalf("frames = %x, %x; want %x, %x", frames[0], frames[1], frame1, frame2)
			}
			if got := p.Stats(); got.Skipped != 2*len(garbage) {
				t.Fatalf("stats = %+v, want Skipped %d", got, 2*len(garbage))
			}
		})
	}
}

// randChunkReader feeds data in random chunks of 1..max bytes.
type randChunkReader struct {
	data []byte
	rnd  *rand.Rand
	max  int
}

func (r *randChunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), 1+r.rnd.Intn(r.max), len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

// chunkReader feeds data in fixed-size chunks.
type chunkReader struct {
	data []byte
	size int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.size, len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

// logRecord is a single captured log call.
type logRecord struct {
	level slog.Level
	msg   string
	args  []any
}

// captureLogger records everything written to it.
type captureLogger struct {
	records []logRecord
}

func (l *captureLogger) Log(_ context.Context, level slog.Level, msg string, args ...any) {
	l.records = append(l.records, logRecord{level: level, msg: msg, args: slices.Clone(args)})
}
