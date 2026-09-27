package cantcp

import (
	"bytes"
	"slices"
	"testing"
)

// Benchmarks keep results alive so the compiler cannot optimize them away.
var (
	benchAdvance int
	benchToken   []byte
	benchPacket  []byte
	benchRaw     []byte
	benchErr     error
	benchType    Type
	benchFrame   Frame
)

// decodeBench is a named stream for the decoder benchmarks.
type decodeBench struct {
	name string
	data []byte
}

// decodeBenchCases returns one-packet and mixed streams for both layouts.
func decodeBenchCases() []decodeBench {
	p := New()
	return []decodeBench{
		{name: "classic packet", data: packet(p, testFrame(8, 1, 2, 3))},
		{name: "can fd packet", data: packet(p, testFrameFd(64, 1, 2, 3))},
		{name: "mixed stream", data: slices.Concat(
			packet(p, testFrame(8, 1)),
			packet(p, testFrameFd(64, 2)),
			packet(p, testFrame(2, 3)),
		)},
	}
}

func BenchmarkSplit(b *testing.B) {
	p := New()
	classic := packet(p, testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8))
	fd := packet(p, testFrameFd(64, 1, 2, 3, 4))
	falseMagic := append(bytes.Repeat([]byte{0xC3, 0xC3, 0x3C}, 8), classic...)
	garbage := bytes.Repeat([]byte{0x01, 0x02, 0x03}, 32)

	tests := []struct {
		name string
		data []byte
	}{
		{name: "classic packet", data: classic},
		{name: "can fd packet", data: fd},
		{name: "garbage without magic", data: garbage},
		{name: "false magic candidates", data: falseMagic},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			p := New()
			b.ReportAllocs()
			for b.Loop() {
				benchAdvance, benchToken, _ = p.Split(tt.data, false)
			}
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	tests := []struct {
		name  string
		frame []byte
	}{
		{name: "classic frame", frame: testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8)},
		{name: "can fd frame", frame: testFrameFd(64, 1, 2, 3, 4)},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			p := New()
			b.ReportAllocs()
			for b.Loop() {
				benchPacket, benchErr = p.Encode(nil, tt.frame)
			}
		})
	}
}

func BenchmarkFrameMarshalBinary(b *testing.B) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{name: "classic frame", frame: Frame{Type: TypeClassic, ID: 0x123, Data: []byte{1, 2, 3, 4}}},
		{name: "can fd frame", frame: Frame{Type: TypeFd, ID: 0x123, BRS: true, Data: bytes.Repeat([]byte{0xAA}, maxFDDataLen)}},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			f := tt.frame
			b.ReportAllocs()
			for b.Loop() {
				benchRaw, benchErr = f.MarshalBinary()
			}
		})
	}
}

func BenchmarkValidateRaw(b *testing.B) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "classic frame", raw: rawClassic(0x123, 8, 1, 2, 3, 4, 5, 6, 7, 8)},
		{name: "can fd frame", raw: rawFd(0x123, 64, canFDBRS, bytes.Repeat([]byte{0xAA}, maxFDDataLen)...)},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchType, benchErr = ValidateRaw(tt.raw)
			}
		})
	}
}

func BenchmarkFrameUnmarshalBinary(b *testing.B) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "classic frame", raw: rawClassic(0x123, 8, 1, 2, 3, 4, 5, 6, 7, 8)},
		{name: "can fd frame", raw: rawFd(0x123, 64, canFDBRS, bytes.Repeat([]byte{0xAA}, maxFDDataLen)...)},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			var f Frame
			b.ReportAllocs()
			for b.Loop() {
				benchErr = f.UnmarshalBinary(tt.raw)
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, tt := range decodeBenchCases() {
		b.Run(tt.name, func(b *testing.B) {
			d := NewDecoder(&cyclicReader{data: tt.data})
			b.ReportAllocs()
			for b.Loop() {
				benchRaw, benchErr = d.Decode()
			}
		})
	}
}

func BenchmarkDecodeFrame(b *testing.B) {
	for _, tt := range decodeBenchCases() {
		b.Run(tt.name, func(b *testing.B) {
			d := NewDecoder(&cyclicReader{data: tt.data})
			b.ReportAllocs()
			for b.Loop() {
				benchFrame, benchErr = d.DecodeFrame()
			}
		})
	}
}

func BenchmarkDecodeFrameInto(b *testing.B) {
	for _, tt := range decodeBenchCases() {
		b.Run(tt.name, func(b *testing.B) {
			d := NewDecoder(&cyclicReader{data: tt.data})
			var f Frame
			b.ReportAllocs()
			for b.Loop() {
				if benchErr = d.DecodeFrameInto(&f); benchErr != nil {
					b.Fatalf("DecodeFrameInto: %v", benchErr)
				}
				benchFrame = f
			}
		})
	}
}
