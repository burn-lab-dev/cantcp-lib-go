package cantcp

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"slices"
	"testing"
)

// errReader fails every Read with a fixed error.
type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

// cyclicReader replays data endlessly and makes no allocation per Read.
type cyclicReader struct {
	data []byte
	pos  int
}

func (r *cyclicReader) Read(p []byte) (int, error) {
	if r.pos == len(r.data) {
		r.pos = 0
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func TestDecode(t *testing.T) {
	build := New()
	classic := testFrame(8, 1, 2, 3, 4, 5, 6, 7, 8)
	fd := testFrameFd(64, 1, 2, 3)
	classicPkt := packet(build, classic)
	fdPkt := packet(build, fd)
	garbage := []byte{0x01, 0x02, 0x03}
	badTypePkt := []byte{0xC3, 0x3C, 0x7F}
	crcBroken := slices.Clone(classicPkt)
	crcBroken[len(crcBroken)-1] ^= 0xFF
	longGarbage := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 25*1024)
	readErr := errors.New("read failed")

	tests := []struct {
		name       string
		reader     io.Reader
		stream     []byte
		opts       []option
		wantFrames [][]byte
		wantErr    error
		wantStats  Stats
	}{
		{
			name:      "empty stream",
			stream:    nil,
			wantErr:   io.EOF,
			wantStats: Stats{},
		},
		{
			name:       "classic frame",
			stream:     classicPkt,
			wantFrames: [][]byte{classic},
			wantErr:    io.EOF,
		},
		{
			name:       "can fd frame",
			stream:     fdPkt,
			wantFrames: [][]byte{fd},
			wantErr:    io.EOF,
		},
		{
			name:       "classic and can fd frames",
			stream:     slices.Concat(classicPkt, fdPkt),
			wantFrames: [][]byte{classic, fd},
			wantErr:    io.EOF,
		},
		{
			name:       "garbage around frames",
			stream:     slices.Concat(garbage, classicPkt, garbage),
			wantFrames: [][]byte{classic},
			wantErr:    io.EOF,
			wantStats:  Stats{Skipped: 6},
		},
		{
			name:       "crc mismatch then valid frame",
			stream:     slices.Concat(crcBroken, classicPkt),
			wantFrames: [][]byte{classic},
			wantErr:    io.EOF,
			wantStats:  Stats{Skipped: 20, Dropped: 1},
		},
		{
			name:       "unknown type skipped",
			stream:     slices.Concat(badTypePkt, classicPkt),
			wantFrames: [][]byte{classic},
			wantErr:    io.EOF,
			wantStats:  Stats{Skipped: 3, BadType: 1},
		},
		{
			name:      "unknown type fails",
			stream:    slices.Concat(badTypePkt, classicPkt),
			opts:      []option{WithBadFramePolicy(BadFrameFail)},
			wantErr:   ErrBadType,
			wantStats: Stats{BadType: 1},
		},
		{
			name:      "bad dlc skipped",
			stream:    packet(build, testFrame(9)),
			wantErr:   io.EOF,
			wantStats: Stats{Skipped: 20, BadDLC: 1},
		},
		{
			name:      "bad dlc fails",
			stream:    packet(build, testFrame(9)),
			opts:      []option{WithBadFramePolicy(BadFrameFail)},
			wantErr:   ErrBadDLC,
			wantStats: Stats{BadDLC: 1},
		},
		{
			name:      "bad can fd len skipped",
			stream:    packet(build, testFrameFd(65)),
			wantErr:   io.EOF,
			wantStats: Stats{Skipped: 76, BadLen: 1},
		},
		{
			name:      "bad can fd len fails",
			stream:    packet(build, testFrameFd(65)),
			opts:      []option{WithBadFramePolicy(BadFrameFail)},
			wantErr:   ErrBadLen,
			wantStats: Stats{BadLen: 1},
		},
		{
			name:      "truncated classic",
			stream:    classicPkt[:len(classicPkt)-5],
			wantErr:   ErrTruncated,
			wantStats: Stats{Truncated: 15},
		},
		{
			name:      "truncated can fd",
			stream:    fdPkt[:len(fdPkt)-1],
			wantErr:   ErrTruncated,
			wantStats: Stats{Truncated: 75},
		},
		{
			name:      "long garbage",
			stream:    longGarbage,
			wantErr:   io.EOF,
			wantStats: Stats{Skipped: len(longGarbage)},
		},
		{
			name:      "read error",
			reader:    errReader{err: readErr},
			wantErr:   readErr,
			wantStats: Stats{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.reader
			if r == nil {
				r = bytes.NewReader(tt.stream)
			}
			d := NewDecoder(r, tt.opts...)

			var frames [][]byte
			var err error
			for {
				var raw []byte
				if raw, err = d.Decode(); err != nil {
					break
				}
				frames = append(frames, raw)
			}
			checkError(t, err, tt.wantErr)
			if len(frames) != len(tt.wantFrames) {
				t.Fatalf("got %d frames, want %d", len(frames), len(tt.wantFrames))
			}
			for i := range frames {
				if !bytes.Equal(frames[i], tt.wantFrames[i]) {
					t.Fatalf("frame %d = %x, want %x", i, frames[i], tt.wantFrames[i])
				}
			}
			if got := d.Stats(); got != tt.wantStats {
				t.Fatalf("stats = %+v, want %+v", got, tt.wantStats)
			}
		})
	}
}

func TestDecodeFrame(t *testing.T) {
	build := New()
	tests := []struct {
		name    string
		raw     []byte
		want    Frame
		wantErr error
	}{
		{
			name: "classic frame",
			raw:  rawClassic(0x123, 2, 0xDE, 0xAD),
			want: Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}},
		},
		{
			name: "classic extended rtr frame",
			raw:  rawClassic(canEFFFlag|canRTRFlag|0x1ABCDE, 0),
			want: Frame{Type: TypeClassic, ID: 0x1ABCDE, EFF: true, RTR: true},
		},
		{
			name: "classic error frame",
			raw:  rawClassic(canERRFlag|0x123, 1, 0xAA),
			want: Frame{Type: TypeClassic, ID: 0x123, ERR: true, Data: []byte{0xAA}},
		},
		{
			name: "can fd frame with flags",
			raw:  rawFd(canEFFFlag|0x1ABCDE, 4, canFDBRS|canFDESI, 1, 2, 3, 4),
			want: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, ESI: true, Data: []byte{1, 2, 3, 4}},
		},
		{
			name: "can fd full payload",
			raw:  rawFd(0x123, 64, 0, testPayload(64)...),
			want: Frame{Type: TypeFd, ID: 0x123, Data: testPayload(64)},
		},
		{
			name:    "can fd non dlc length",
			raw:     rawFd(0x123, 9, 0),
			wantErr: ErrBadLen,
		},
		{
			name:    "can fd bad flags",
			raw:     rawFd(0x123, 4, 0x80),
			wantErr: ErrBadFlags,
		},
		{
			name:    "can fd rtr",
			raw:     rawFd(canRTRFlag|0x123, 4, 0),
			wantErr: ErrBadFlags,
		},
		{
			name: "classic reserved byte",
			raw: func() []byte {
				b := rawClassic(0x123, 0)
				b[res0Off] = 1
				return b
			}(),
			wantErr: ErrReserved,
		},
		{
			name:    "classic bad identifier",
			raw:     rawClassic(0x800, 0),
			wantErr: ErrBadID,
		},
		{
			name:    "classic dlc above 8 is dropped by the splitter",
			raw:     rawClassic(0x123, 9),
			wantErr: io.EOF,
		},
		{
			name:    "can fd len above 64 is dropped by the splitter",
			raw:     rawFd(0x123, 65, 0),
			wantErr: io.EOF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDecoder(bytes.NewReader(packet(build, tt.raw)))
			got, err := d.DecodeFrame()
			checkError(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			checkFrame(t, got, tt.want)
			if _, err := d.DecodeFrame(); !errors.Is(err, io.EOF) {
				t.Fatalf("second DecodeFrame err = %v, want io.EOF", err)
			}
		})
	}
}

func TestDecodeFrameInto(t *testing.T) {
	build := New()
	tests := []struct {
		name   string
		prefix []byte
		suffix []byte
		frames []struct {
			raw  []byte
			want Frame
		}
	}{
		{
			name: "classic and can fd",
			frames: []struct {
				raw  []byte
				want Frame
			}{
				{
					raw:  rawClassic(0x123, 2, 0xDE, 0xAD),
					want: Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}},
				},
				{
					raw:  rawFd(canEFFFlag|0x1ABCDE, 4, 0, 1, 2, 3, 4),
					want: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, Data: []byte{1, 2, 3, 4}},
				},
			},
		},
		{
			name:   "garbage around the frames",
			prefix: []byte{0x01, 0x02},
			suffix: []byte{0x03},
			frames: []struct {
				raw  []byte
				want Frame
			}{
				{
					raw:  rawClassic(canEFFFlag|canRTRFlag|0x1ABCDE, 0),
					want: Frame{Type: TypeClassic, ID: 0x1ABCDE, EFF: true, RTR: true},
				},
			},
		},
		{
			name: "reuse after a full can fd payload",
			frames: []struct {
				raw  []byte
				want Frame
			}{
				{
					raw:  rawFd(canEFFFlag|0x1ABCDE, 64, canFDBRS|canFDESI, testPayload(64)...),
					want: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, ESI: true, Data: testPayload(64)},
				},
				{
					raw:  rawClassic(0x456, 1, 0xAA),
					want: Frame{Type: TypeClassic, ID: 0x456, Data: []byte{0xAA}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := slices.Clone(tt.prefix)
			for _, fc := range tt.frames {
				stream = append(stream, packet(build, fc.raw)...)
			}
			stream = append(stream, tt.suffix...)
			d := NewDecoder(bytes.NewReader(stream))

			var f Frame
			for i, fc := range tt.frames {
				checkError(t, d.DecodeFrameInto(&f), nil)
				checkFrame(t, f, fc.want)
				raw, err := f.MarshalBinary()
				checkError(t, err, nil)
				if !bytes.Equal(raw, fc.raw) {
					t.Fatalf("frame %d raw = %x, want %x", i, raw, fc.raw)
				}
			}

			// io.EOF must leave the frame untouched.
			checkError(t, d.DecodeFrameInto(&f), io.EOF)
			checkFrame(t, f, tt.frames[len(tt.frames)-1].want)
		})
	}
}

func TestDecodeFrameIntoKeepsFrameOnError(t *testing.T) {
	build := New()
	readErr := errors.New("read failed")
	truncated := packet(build, rawFd(0x123, 64, 0))
	truncated = truncated[:len(truncated)-1]
	// The splitter is tolerant (canfd len <= 64), the Frame model is strict.
	strictLen := packet(build, rawFd(0x123, 9, 0))

	tests := []struct {
		name    string
		reader  io.Reader
		wantErr error
	}{
		{name: "empty stream", reader: bytes.NewReader(nil), wantErr: io.EOF},
		{name: "read error", reader: errReader{err: readErr}, wantErr: readErr},
		{name: "truncated packet", reader: bytes.NewReader(truncated), wantErr: ErrTruncated},
		{name: "strict can fd length", reader: bytes.NewReader(strictLen), wantErr: ErrBadLen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f Frame
			checkError(t, f.UnmarshalBinary(rawClassic(0x321, 1, 0xAA)), nil)
			before, err := f.MarshalBinary()
			checkError(t, err, nil)

			d := NewDecoder(tt.reader)
			checkError(t, d.DecodeFrameInto(&f), tt.wantErr)

			checkFrame(t, f, Frame{Type: TypeClassic, ID: 0x321, Data: []byte{0xAA}})
			after, err := f.MarshalBinary()
			checkError(t, err, nil)
			if !bytes.Equal(after, before) {
				t.Fatalf("frame changed on error: %x, want %x", after, before)
			}
		})
	}
}

func TestDecodeFrameIntoIndependentOfBuffer(t *testing.T) {
	build := New()
	frame1 := rawClassic(0x123, 2, 0xDE, 0xAD)
	frame2 := rawFd(0x456, 4, 0, 1, 2, 3, 4)
	d := NewDecoder(bytes.NewReader(slices.Concat(packet(build, frame1), packet(build, frame2))))

	var first Frame
	checkError(t, d.DecodeFrameInto(&first), nil)
	var second Frame
	checkError(t, d.DecodeFrameInto(&second), nil)

	// first must not be a window into the decoder buffer: reading the next
	// frame must not change it.
	checkFrame(t, first, Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}})
	checkFrame(t, second, Frame{Type: TypeFd, ID: 0x456, Data: []byte{1, 2, 3, 4}})
	raw, err := first.MarshalBinary()
	checkError(t, err, nil)
	if !bytes.Equal(raw, frame1) {
		t.Fatalf("first raw = %x, want %x (aliased the decoder buffer?)", raw, frame1)
	}
}

func TestDecodeFrameIntoAllocations(t *testing.T) {
	build := New()
	d := NewDecoder(&cyclicReader{data: bytes.Repeat(packet(build, testFrame(8, 1, 2, 3)), 64)})

	var f Frame
	allocs := testing.AllocsPerRun(100, func() {
		if err := d.DecodeFrameInto(&f); err != nil {
			t.Fatalf("DecodeFrameInto: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per frame = %v, want 0", allocs)
	}
}

func TestDecodeCopiesFrame(t *testing.T) {
	build := New()
	frame1 := testFrame(8, 1, 2, 3)
	frame2 := testFrameFd(64, 4, 5, 6)
	d := NewDecoder(bytes.NewReader(slices.Concat(packet(build, frame1), packet(build, frame2))))

	first, err := d.Decode()
	checkError(t, err, nil)
	second, err := d.Decode()
	checkError(t, err, nil)

	// The first frame must not be a window into the decoder buffer: reading
	// the next frame must not change it.
	if !bytes.Equal(first, frame1) {
		t.Fatalf("first frame = %x, want %x (aliased the decoder buffer?)", first, frame1)
	}
	clear(first)
	if !bytes.Equal(second, frame2) {
		t.Fatalf("second frame = %x, want %x", second, frame2)
	}
	if _, err := d.Decode(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Decode err = %v, want io.EOF", err)
	}
}

func TestDecoderStatsReset(t *testing.T) {
	frame := testFrame(8, 1, 2, 3)
	garbage := []byte{0x01, 0x02, 0x03}
	d := NewDecoder(bytes.NewReader(slices.Concat(garbage, packet(New(), frame))))

	if _, err := d.Decode(); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got, want := d.Stats(), (Stats{Skipped: len(garbage)}); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
	d.ResetStats()
	if got := d.Stats(); got != (Stats{}) {
		t.Fatalf("stats after reset = %+v, want zero", got)
	}
}

func TestDecoderOptions(t *testing.T) {
	tests := []struct {
		name string
		opts []option
	}{
		{name: "default"},
		{name: "custom magic", opts: []option{WithMagic(0x11, 0x22)}},
		{name: "custom crc polynomial", opts: []option{WithCRCPoly(0x1D)}},
		{name: "crc covers the frame only", opts: []option{WithCRCCoverFrameOnly()}},
		{name: "bad frame fail", opts: []option{WithBadFramePolicy(BadFrameFail)}},
		{name: "logger", opts: []option{WithLogger(&captureLogger{}), WithLogLevel(LevelTrace)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := New(tt.opts...)
			frame := testFrame(8, 1, 2, 3)
			d := NewDecoder(bytes.NewReader(packet(build, frame)), tt.opts...)

			got, err := d.Decode()
			checkError(t, err, nil)
			if !bytes.Equal(got, frame) {
				t.Fatalf("frame = %x, want %x", got, frame)
			}
			if _, err := d.Decode(); !errors.Is(err, io.EOF) {
				t.Fatalf("second Decode err = %v, want io.EOF", err)
			}
		})
	}
}

func TestDecoderChunked(t *testing.T) {
	build := New()
	frame1 := testFrame(8, 1, 2, 3)
	frame2 := testFrameFd(64, 4, 5, 6)
	garbage := []byte{0x01, 0x02, 0x03}
	stream := slices.Concat(garbage, packet(build, frame1), packet(build, frame2), garbage)

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
			d := NewDecoder(&chunkReader{data: slices.Clone(stream), size: tt.chunkSize})
			frame, err := d.Decode()
			checkError(t, err, nil)
			if !bytes.Equal(frame, frame1) {
				t.Fatalf("frame 0 = %x, want %x", frame, frame1)
			}
			frame, err = d.Decode()
			checkError(t, err, nil)
			if !bytes.Equal(frame, frame2) {
				t.Fatalf("frame 1 = %x, want %x", frame, frame2)
			}
			if _, err := d.Decode(); !errors.Is(err, io.EOF) {
				t.Fatalf("last Decode err = %v, want io.EOF", err)
			}
			if got, want := d.Stats(), (Stats{Skipped: 2 * len(garbage)}); got != want {
				t.Fatalf("stats = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDecoderFragmentation(t *testing.T) {
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
			r := &randChunkReader{
				data: slices.Clone(stream),
				rnd:  rand.New(rand.NewSource(tt.seed)),
				max:  tt.max,
			}
			d := NewDecoder(r)
			frame, err := d.Decode()
			checkError(t, err, nil)
			if !bytes.Equal(frame, frame1) {
				t.Fatalf("frame 0 = %x, want %x", frame, frame1)
			}
			frame, err = d.Decode()
			checkError(t, err, nil)
			if !bytes.Equal(frame, frame2) {
				t.Fatalf("frame 1 = %x, want %x", frame, frame2)
			}
			if _, err := d.Decode(); !errors.Is(err, io.EOF) {
				t.Fatalf("last Decode err = %v, want io.EOF", err)
			}
			if got, want := d.Stats(), (Stats{Skipped: 2 * len(garbage)}); got != want {
				t.Fatalf("stats = %+v, want %+v", got, want)
			}
		})
	}
}
