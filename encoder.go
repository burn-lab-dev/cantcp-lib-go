package cantcp

import "io"

// encoder writes framed CAN packets to a byte stream:
//
//	[magic 2 bytes][type 1 byte][struct can_frame 16 | canfd_frame 72][CRC-8 1]
//
// It wraps Encode: the frame length selects the packet type, the CRC and its
// coverage follow the parser configuration, so a packet written by an encoder
// is always accepted by a decoder created with the same options. An encoder is
// not safe for concurrent use: use one encoder per stream.
//
// An encoder is created only by NewEncoder. The type itself is not exported:
// the API surface is NewEncoder together with the exported methods.
type encoder struct {
	w   io.Writer
	p   *parser
	buf []byte
}

// NewEncoder returns an encoder writing packets to w. Options configure it
// exactly like New.
//
// The returned type is not exported, like the parser of New: an encoder is
// used through the returned value and its exported methods only.
func NewEncoder(w io.Writer, opts ...option) *encoder {
	return &encoder{w: w, p: New(opts...)}
}

// Encode writes a packet carrying the raw frame: 16 bytes build a TypeClassic
// packet, 72 bytes a TypeFd packet. Validation errors of Encode (ErrFrameLen,
// ErrBadDLC, ErrBadLen) leave the stream untouched and the buffer reusable.
//
// A short write returns io.ErrShortWrite; a write error is returned unchanged
// and leaves the stream in an unknown state.
func (e *encoder) Encode(frame []byte) error {
	buf, err := e.p.Encode(e.buf[:0], frame)
	if err != nil {
		return err
	}
	e.buf = buf
	n, err := e.w.Write(buf)
	if err != nil {
		return err
	}
	if n < len(buf) {
		return io.ErrShortWrite
	}
	return nil
}

// EncodeFrame marshals f and writes the packet. Marshal errors (ErrBadType,
// ErrBadID, ErrBadFlags, ErrBadDLC, ErrBadLen) leave the stream untouched.
func (e *encoder) EncodeFrame(f *Frame) error {
	raw, err := f.MarshalBinary()
	if err != nil {
		return err
	}
	return e.Encode(raw)
}
