package cantcp

import (
	"bufio"
	"io"
	"slices"
)

// decoderBufSize is the Scanner buffer size. A packet is never longer than
// fdPktLen, so a full buffer always lets Split advance and the decoder never
// reports bufio.ErrTooLong, however long the garbage in the stream is.
const decoderBufSize = fdPktLen

// decoder reads a byte stream of framed CAN packets:
//
//	[magic 2 bytes][type 1 byte][struct can_frame 16 | canfd_frame 72][CRC-8 1]
//
// It wraps Split: garbage and packets rejected by CRC are dropped, the stream
// counters are collected and every Decode call returns one raw frame. A decoder
// is not safe for concurrent use: use one decoder per stream.
//
// A decoder is created only by NewDecoder; the parser, the CRC-8 table and the
// counters are private to it. The type itself is not exported: the API surface
// is NewDecoder together with the exported methods.
type decoder struct {
	sc *bufio.Scanner
	p  *parser
}

// NewDecoder returns a decoder reading packets from r. Options configure it
// exactly like New: the defaults are magic 0xC3 0x3C, CRC-8 polynomial 0x07
// covering magic+type+frame, BadFrameSkip policy and logging disabled at level
// Info.
//
// The returned type is not exported, like the parser of New: a decoder is used
// through the returned value and its exported methods only.
func NewDecoder(r io.Reader, opts ...option) *decoder {
	p := New(opts...)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, decoderBufSize), decoderBufSize)
	sc.Split(p.Split)
	return &decoder{sc: sc, p: p}
}

// Decode returns the next raw frame: 16 bytes for classic CAN, 72 bytes for
// CAN FD. The slice is independent of the decoder buffer and stays valid after
// the next call.
//
// The end of the stream returns io.EOF; a packet cut off in the middle returns
// ErrTruncated; read errors are returned unchanged. A closed net.Conn surfaces
// as net.ErrClosed, so a graceful shutdown can be told apart both from a clean
// close (io.EOF) and from a truncated packet (ErrTruncated).
func (d *decoder) Decode() ([]byte, error) {
	raw, err := d.scanRaw()
	if err != nil {
		return nil, err
	}
	return slices.Clone(raw), nil
}

// DecodeFrame returns the next frame parsed into a Frame. It is a convenience
// wrapper around DecodeFrameInto: use DecodeFrameInto to parse into a
// caller-owned Frame and make no per-frame allocation.
//
// The splitter stays tolerant (canfd len up to 64) while the Frame model is
// strict, so a canfd len that the 4-bit DLC field cannot encode is returned as
// ErrBadLen even though the splitter accepted it.
//
// The returned frame is independent of the decoder: it keeps its own raw
// frame, so Data stays valid after the next call.
func (d *decoder) DecodeFrame() (Frame, error) {
	var f Frame
	if err := d.DecodeFrameInto(&f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// DecodeFrameInto parses the next frame into f, reusing its storage. A caller
// that passes the same f in a loop makes no per-frame allocation: the raw
// frame is copied straight into f's own raw storage, so the returned Data
// window points into f and stays valid after the next call.
//
// The error returns of Decode apply unchanged. On error f is left untouched
// (the strict UnmarshalBinary commits only after all checks pass), so a caller
// may keep the previous contents and inspect f only on a nil error.
//
// The parsing is as strict as DecodeFrame: a canfd len that the 4-bit DLC
// field cannot encode is returned as ErrBadLen even though the splitter
// accepted it. f must not be copied while Data is in use: a Frame keeps its
// raw storage in the value, and a copy keeps pointing into the original.
func (d *decoder) DecodeFrameInto(f *Frame) error {
	raw, err := d.scanRaw()
	if err != nil {
		return err
	}
	return f.UnmarshalBinary(raw)
}

// scanRaw returns the next raw frame as a window into the Scanner buffer. The
// window is valid only until the next Scan, so callers must copy it: Decode
// clones it, DecodeFrameInto unmarshals it into the Frame's own raw storage.
func (d *decoder) scanRaw() ([]byte, error) {
	if !d.sc.Scan() {
		if err := d.sc.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return d.sc.Bytes(), nil
}

// Stats returns a copy of the decoder stream counters.
func (d *decoder) Stats() Stats {
	return d.p.Stats()
}

// ResetStats zeroes the decoder stream counters.
func (d *decoder) ResetStats() {
	d.p.ResetStats()
}
