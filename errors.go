package cantcp

import "errors"

var (
	// ErrBadDLC reports a classic can_frame whose can_dlc is greater than 8.
	ErrBadDLC = errors.New("cantcp: can_dlc > 8")
	// ErrBadLen reports an invalid CAN FD length: canfd len > 64, or a len
	// that cannot be encoded in the 4-bit DLC field (9..11, 13..15, ...).
	ErrBadLen = errors.New("cantcp: invalid canfd length")
	// ErrBadType reports an unknown frame type: the packet type byte is not
	// TypeClassic or TypeFd, or Frame.Type is not set to one of them.
	ErrBadType = errors.New("cantcp: unknown frame type")
	// ErrBadFlags reports flags that are not valid for the frame type.
	ErrBadFlags = errors.New("cantcp: flags are not valid for the frame type")
	// ErrBadID reports an identifier that does not fit the addressing mode:
	// more than 11 bits without FlagEFF, or more than 29 bits with it.
	ErrBadID = errors.New("cantcp: identifier does not fit the addressing mode")
	// ErrReserved reports non-zero reserved bytes of a frame.
	ErrReserved = errors.New("cantcp: reserved bytes are not zero")
	// ErrFrameLen reports a frame whose length is neither 16 (can_frame)
	// nor 72 (canfd_frame) bytes.
	ErrFrameLen = errors.New("cantcp: frame length is not 16 or 72 bytes")
	// ErrTruncated reports an incomplete packet at the end of the stream.
	ErrTruncated = errors.New("cantcp: stream truncated in the middle of a packet")
)
