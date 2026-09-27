package cantcp

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

// writeRecorder captures everything written to it. A positive limit makes
// Write report limit bytes written; err, if set, is returned after the write.
type writeRecorder struct {
	data  []byte
	err   error
	limit int
}

func (w *writeRecorder) Write(p []byte) (int, error) {
	n := len(p)
	if w.limit > 0 && w.limit < n {
		n = w.limit
	}
	w.data = append(w.data, p[:n]...)
	return n, w.err
}

func TestEncoderEncode(t *testing.T) {
	tests := []struct {
		name    string
		frame   []byte
		wantErr error
	}{
		{name: "classic frame", frame: testFrame(8, 1, 2, 3)},
		{name: "can fd frame", frame: testFrameFd(64, 1, 2, 3)},
		{name: "empty frame", frame: []byte{}, wantErr: ErrFrameLen},
		{name: "short frame", frame: make([]byte, frameLen-1), wantErr: ErrFrameLen},
		{name: "long frame", frame: make([]byte, fdFrameLen+1), wantErr: ErrFrameLen},
		{name: "bad dlc", frame: testFrame(9), wantErr: ErrBadDLC},
		{name: "bad can fd len", frame: testFrameFd(65), wantErr: ErrBadLen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writeRecorder{}
			e := NewEncoder(w)
			checkError(t, e.Encode(tt.frame), tt.wantErr)
			if tt.wantErr != nil {
				if len(w.data) != 0 {
					t.Fatalf("writer got %x, want nothing", w.data)
				}
				return
			}
			want := packet(New(), tt.frame)
			if !bytes.Equal(w.data, want) {
				t.Fatalf("packet = %x, want %x", w.data, want)
			}
		})
	}
}

func TestEncoderEncodeFrame(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr error
	}{
		{
			name:  "classic frame",
			frame: Frame{Type: TypeClassic, ID: 0x123, EFF: true, Data: []byte{1, 2}},
		},
		{
			name:  "can fd frame",
			frame: Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, ESI: true, Data: []byte{1, 2, 3}},
		},
		{name: "unset type", frame: Frame{}, wantErr: ErrBadType},
		{name: "rtr in can fd", frame: Frame{Type: TypeFd, ID: 0x123, RTR: true}, wantErr: ErrBadFlags},
		{name: "identifier does not fit", frame: Frame{Type: TypeClassic, ID: 0x800}, wantErr: ErrBadID},
		{name: "classic dlc above 8", frame: Frame{Type: TypeClassic, ID: 0x123, Data: make([]byte, 9)}, wantErr: ErrBadDLC},
		{name: "can fd non dlc length", frame: Frame{Type: TypeFd, ID: 0x123, Data: make([]byte, 9)}, wantErr: ErrBadLen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writeRecorder{}
			e := NewEncoder(w)
			f := tt.frame
			checkError(t, e.EncodeFrame(&f), tt.wantErr)
			if tt.wantErr != nil {
				if len(w.data) != 0 {
					t.Fatalf("writer got %x, want nothing", w.data)
				}
				return
			}
			raw, err := f.MarshalBinary()
			checkError(t, err, nil)
			want := packet(New(), raw)
			if !bytes.Equal(w.data, want) {
				t.Fatalf("packet = %x, want %x", w.data, want)
			}
		})
	}
}

func TestEncoderWriteErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	frame := testFrame(8, 1, 2, 3)
	full := packet(New(), frame)

	tests := []struct {
		name       string
		rec        *writeRecorder
		wantErr    error
		wantPrefix int
	}{
		{
			name:       "short write",
			rec:        &writeRecorder{limit: len(full) - 1},
			wantErr:    io.ErrShortWrite,
			wantPrefix: len(full) - 1,
		},
		{
			name:       "write error after a partial write",
			rec:        &writeRecorder{err: writeErr, limit: 2},
			wantErr:    writeErr,
			wantPrefix: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEncoder(tt.rec)
			checkError(t, e.Encode(frame), tt.wantErr)
			if !bytes.Equal(tt.rec.data, full[:tt.wantPrefix]) {
				t.Fatalf("written = %x, want prefix %x", tt.rec.data, full[:tt.wantPrefix])
			}
		})
	}
}

func TestEncoderOptions(t *testing.T) {
	tests := []struct {
		name string
		opts []option
	}{
		{name: "default"},
		{name: "custom magic", opts: []option{WithMagic(0x11, 0x22)}},
		{name: "custom crc polynomial", opts: []option{WithCRCPoly(0x1D)}},
		{name: "crc covers the frame only", opts: []option{WithCRCCoverFrameOnly()}},
		{name: "logger", opts: []option{WithLogger(&captureLogger{}), WithLogLevel(LevelTrace)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := New(tt.opts...)
			frame := testFrame(8, 1, 2, 3)
			w := &writeRecorder{}
			e := NewEncoder(w, tt.opts...)

			checkError(t, e.Encode(frame), nil)
			if want := packet(build, frame); !bytes.Equal(w.data, want) {
				t.Fatalf("packet = %x, want %x", w.data, want)
			}
		})
	}
}

func TestEncoderReuse(t *testing.T) {
	build := New()
	frames := [][]byte{testFrame(8, 1), testFrameFd(64, 2), testFrame(2, 3)}
	w := &writeRecorder{}
	e := NewEncoder(w)

	var want []byte
	for _, frame := range frames {
		checkError(t, e.Encode(frame), nil)
		want = slices.Concat(want, packet(build, frame))
	}
	if !bytes.Equal(w.data, want) {
		t.Fatalf("packets = %x, want %x", w.data, want)
	}
}
