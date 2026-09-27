package cantcp

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

func FuzzSplit(f *testing.F) {
	build := New()
	f.Add(packet(build, testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8)))
	f.Add(packet(build, testFrameFd(64, 1, 2, 3)))
	f.Add([]byte{0xC3, 0x3C, byte(TypeClassic)})
	f.Add([]byte{0xC3, 0x3C, byte(TypeFd)})
	f.Add([]byte{0xC3, 0x3C, 0x7F})
	f.Add([]byte{0xC3})
	f.Add([]byte{0xC3, 0xC3, 0x3C})
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02, 0x03})
	f.Add(make([]byte, classicPktLen))
	f.Add(make([]byte, fdPktLen))
	f.Add(bytes.Repeat([]byte{0xC3, 0x3C}, 16))

	optionSets := []struct {
		name string
		opts []option
	}{
		{name: "default"},
		{name: "same magic bytes", opts: []option{WithMagic(0xC3, 0xC3)}},
		{name: "zero magic", opts: []option{WithMagic(0x00, 0x00)}},
		{name: "frame only crc", opts: []option{WithCRCCoverFrameOnly()}},
		{name: "bad frame fail", opts: []option{WithBadFramePolicy(BadFrameFail)}},
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		for _, set := range optionSets {
			for _, atEOF := range []bool{false, true} {
				p := New(set.opts...)
				advance, token, err := p.Split(data, atEOF)
				if advance < 0 || advance > len(data) {
					t.Fatalf("%s: advance = %d, len(data) = %d", set.name, advance, len(data))
				}
				if token != nil && len(token) != frameLen && len(token) != fdFrameLen {
					t.Fatalf("%s: token length = %d, want %d or %d", set.name, len(token), frameLen, fdFrameLen)
				}
				if err != nil && token != nil {
					t.Fatalf("%s: token returned together with an error", set.name)
				}
				switch {
				case err == nil,
					errors.Is(err, ErrTruncated),
					errors.Is(err, ErrBadDLC),
					errors.Is(err, ErrBadLen),
					errors.Is(err, ErrBadType):
				default:
					t.Fatalf("%s: unexpected error: %v", set.name, err)
				}
			}
		}
	})
}

func FuzzEncodeSplit(f *testing.F) {
	f.Add(make([]byte, frameLen))
	f.Add(make([]byte, fdFrameLen))
	f.Add(testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8))
	f.Add(testFrameFd(64, 1, 2, 3))
	f.Add(testFrame(0))
	f.Add(testFrameFd(0))

	optionSets := []struct {
		name string
		opts []option
	}{
		{name: "default"},
		{name: "custom magic", opts: []option{WithMagic(0x11, 0x22)}},
		{name: "same magic bytes", opts: []option{WithMagic(0xC3, 0xC3)}},
		{name: "zero magic", opts: []option{WithMagic(0x00, 0x00)}},
		{name: "custom polynomial", opts: []option{WithCRCPoly(0x1D)}},
		{name: "frame only crc", opts: []option{WithCRCCoverFrameOnly()}},
	}

	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) != frameLen && len(frame) != fdFrameLen {
			t.Skip()
		}
		frame = bytes.Clone(frame)
		limit := byte(maxDLC)
		if len(frame) == fdFrameLen {
			limit = maxFDDataLen
		}
		if frame[dlcOff] > limit {
			frame[dlcOff] = 0
		}
		for _, set := range optionSets {
			p := New(set.opts...)
			pkt, err := p.Encode(nil, frame)
			if err != nil {
				t.Fatalf("%s: Encode: %v", set.name, err)
			}
			if len(pkt) != frameOff+len(frame)+crcLen {
				t.Fatalf("%s: packet length = %d, want %d", set.name, len(pkt), frameOff+len(frame)+crcLen)
			}
			advance, token, err := p.Split(pkt, true)
			if err != nil {
				t.Fatalf("%s: Split: %v", set.name, err)
			}
			if advance != len(pkt) {
				t.Fatalf("%s: advance = %d, want %d", set.name, advance, len(pkt))
			}
			if !bytes.Equal(token, frame) {
				t.Fatalf("%s: token = %x, want %x", set.name, token, frame)
			}
			if got := p.Stats(); got != (Stats{}) {
				t.Fatalf("%s: stats = %+v, want zero", set.name, got)
			}
		}
	})
}

func FuzzFrameUnmarshal(f *testing.F) {
	f.Add(make([]byte, frameLen))
	f.Add(make([]byte, fdFrameLen))
	f.Add(rawClassic(0x123, 2, 0xDE, 0xAD))
	f.Add(rawClassic(canEFFFlag|0x1ABCDE, 8, 1, 2, 3, 4, 5, 6, 7, 8))
	f.Add(rawFd(0x123, 64, canFDBRS|canFDESI))
	f.Add(rawFd(canEFFFlag|0x1ABCDE, 4, 0, 1, 2, 3, 4))
	f.Add(make([]byte, 15))
	f.Add(make([]byte, 73))

	f.Fuzz(func(t *testing.T, b []byte) {
		var f Frame
		err := f.UnmarshalBinary(b)
		if err != nil {
			switch {
			case errors.Is(err, ErrFrameLen),
				errors.Is(err, ErrBadDLC),
				errors.Is(err, ErrBadLen),
				errors.Is(err, ErrBadFlags),
				errors.Is(err, ErrBadID),
				errors.Is(err, ErrReserved):
				return
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		// MarshalBinary canonicalizes the data area: bytes beyond the length
		// field are not part of Data and are written as zeroes.
		canonical := bytes.Clone(b)
		clear(canonical[dataOff+int(canonical[dlcOff]):])
		raw, err := f.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary after successful UnmarshalBinary: %v", err)
		}
		if !bytes.Equal(raw, canonical) {
			t.Fatalf("raw = %x, want canonical %x", raw, canonical)
		}
		var again Frame
		if err := again.UnmarshalBinary(raw); err != nil {
			t.Fatalf("UnmarshalBinary(MarshalBinary): %v", err)
		}
		checkFrame(t, again, f)
		if got := again.GetRaw(); !bytes.Equal(got, raw) {
			t.Fatalf("GetRaw = %x, want %x", got, raw)
		}
	})
}

func FuzzValidateRaw(f *testing.F) {
	f.Add(make([]byte, frameLen))
	f.Add(make([]byte, fdFrameLen))
	f.Add(rawClassic(0x123, 2, 0xDE, 0xAD))
	f.Add(rawClassic(canEFFFlag|0x1ABCDE, 8, 1, 2, 3, 4, 5, 6, 7, 8))
	f.Add(rawClassic(canERRFlag|canEFFMask, 0))
	f.Add(rawClassic(canERRFlag|canEFFFlag|0x123, 0))
	f.Add(rawClassic(canERRFlag|canRTRFlag|0x123, 0))
	f.Add(rawFd(0x123, 64, canFDBRS|canFDESI))
	f.Add(rawFd(canRTRFlag|0x1, 0, 0))
	f.Add(rawFd(canERRFlag|0x1ABCDE, 0, canFDBRS))
	f.Add(make([]byte, frameLen-1))
	f.Add(make([]byte, fdFrameLen+1))

	f.Fuzz(func(t *testing.T, b []byte) {
		typ, err := ValidateRaw(b)

		var f Frame
		frameErr := f.UnmarshalBinary(b)
		if (err == nil) != (frameErr == nil) {
			t.Fatalf("ValidateRaw err = %v, UnmarshalBinary err = %v", err, frameErr)
		}
		switch len(b) {
		case frameLen:
			if typ != TypeClassic {
				t.Fatalf("type = %v, want %v", typ, TypeClassic)
			}
		case fdFrameLen:
			if typ != TypeFd {
				t.Fatalf("type = %v, want %v", typ, TypeFd)
			}
		default:
			if err == nil {
				t.Fatal("ValidateRaw accepted a frame of invalid length")
			}
			if typ != 0 {
				t.Fatalf("type = %v for an invalid length, want unset", typ)
			}
			return
		}
		if err != nil {
			return
		}
		if f.Type != typ {
			t.Fatalf("frame type = %v, ValidateRaw type = %v", f.Type, typ)
		}
	})
}

func FuzzDecoder(f *testing.F) {
	build := New()
	f.Add(packet(build, testFrame(8, 1, 2, 3)))
	f.Add(packet(build, testFrameFd(64, 1, 2, 3)))
	f.Add(packet(build, testFrameFd(9)))
	f.Add(slices.Concat([]byte{0x01, 0x02}, packet(build, testFrame(2, 3))))
	f.Add([]byte{0xC3, 0x3C, 0x7F})
	f.Add([]byte{0xC3})
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xC3, 0x3C}, 16))

	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewDecoder(bytes.NewReader(data))
		for range len(data) + 2 {
			raw, err := d.Decode()
			if err != nil {
				switch {
				case errors.Is(err, io.EOF),
					errors.Is(err, ErrTruncated),
					errors.Is(err, ErrBadDLC),
					errors.Is(err, ErrBadLen),
					errors.Is(err, ErrBadType):
					return
				default:
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if len(raw) != frameLen && len(raw) != fdFrameLen {
				t.Fatalf("frame length = %d, want %d or %d", len(raw), frameLen, fdFrameLen)
			}
		}
	})
}

func FuzzDecodeFrameInto(f *testing.F) {
	build := New()
	f.Add(packet(build, testFrame(8, 1, 2, 3)))
	f.Add(packet(build, testFrameFd(64, 1, 2, 3)))
	f.Add(packet(build, testFrameFd(9)))
	f.Add(slices.Concat(packet(build, testFrame(2, 3)), packet(build, testFrameFd(9))))
	f.Add([]byte{0xC3})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		rawDecoder := NewDecoder(bytes.NewReader(data))
		frameDecoder := NewDecoder(bytes.NewReader(data))

		var fr Frame
		for range len(data) + 2 {
			raw, rawErr := rawDecoder.Decode()
			err := frameDecoder.DecodeFrameInto(&fr)

			switch {
			case rawErr == nil && err == nil:
				if got := fr.GetRaw(); !bytes.Equal(got, raw) {
					t.Fatalf("GetRaw = %x, Decode = %x", got, raw)
				}
				if got := len(fr.Data); got != int(raw[dlcOff]) {
					t.Fatalf("len(Data) = %d, length field = %d", got, raw[dlcOff])
				}
			case rawErr == nil:
				// The splitter accepts more than the strict Frame model:
				// canfd lengths outside the 4-bit DLC scale and raw frames
				// with invalid flags, identifiers or reserved bytes.
				switch {
				case errors.Is(err, ErrBadLen), errors.Is(err, ErrBadFlags),
					errors.Is(err, ErrBadID), errors.Is(err, ErrReserved):
					continue
				default:
					t.Fatalf("DecodeFrameInto error = %v, want a strict Frame error", err)
				}
			case err == nil:
				t.Fatalf("Decode = %v, DecodeFrameInto = nil", rawErr)
			default:
				if !errors.Is(rawErr, err) {
					t.Fatalf("errors diverged: Decode = %v, DecodeFrameInto = %v", rawErr, err)
				}
				return
			}
		}
	})
}
