package cantcp

import "encoding/binary"

// ValidateRaw validates a raw Linux SocketCAN frame without the cantcp stream
// envelope and reports its type: 16 bytes are decoded as a struct can_frame
// (TypeClassic), 72 bytes as a struct canfd_frame (TypeFd). Any other length
// returns ErrFrameLen and leaves the type unset.
//
// The layout is selected by the length only: a raw SocketCAN frame carries no
// separate CAN FD marker, the frame type is defined by the socket it was read
// from.
//
// Validation is strict and follows Linux SocketCAN:
//
//   - an identifier that does not fit the addressing mode returns ErrBadID:
//     more than 11 bits without CAN_EFF_FLAG (CAN_SFF_MASK), no silent
//     masking. Error frames carry a 29-bit error class mask instead of an
//     identifier, so they are not checked against the 11-bit mask;
//   - error frames carry no addressing mode: CAN_ERR_FLAG combined with
//     CAN_EFF_FLAG or CAN_RTR_FLAG returns ErrBadFlags (can_err_mask_t keeps
//     bits 29-31 zero), while error class bits above 11 bits are valid;
//   - CAN_RTR_FLAG is not valid for CAN FD and returns ErrBadFlags;
//   - classic can_dlc > 8 returns ErrBadDLC;
//   - a CAN FD len that the 4-bit DLC field cannot encode returns ErrBadLen;
//   - unknown bits of the CAN FD flags byte return ErrBadFlags;
//   - non-zero padding and reserved bytes return ErrReserved: __pad, __res0
//     and len8_dlc for classic, __res0 and __res1 for CAN FD.
//
// Bytes of the data area beyond the length field are not validated: SocketCAN
// does not guarantee zeroes there, so they are not part of the frame. All
// checks are bit operations on the raw bytes: no copy and no allocation is
// made.
//
// The returned Type is the layout selected by the length even when err is not
// nil, as long as the length is 16 or 72 bytes, so a caller can tell a classic
// frame from a CAN FD one. On ErrFrameLen the type is unset.
func ValidateRaw(b []byte) (Type, error) {
	typ, _, err := checkRaw(b)
	return typ, err
}

// checkRaw is the single source of truth of raw frame validation: ValidateRaw
// and Frame.UnmarshalBinary both go through it. It returns the length-selected
// type and the raw can_id word. The type is set whenever the length is 16 or
// 72 bytes, even on error; the raw word is zero on error.
func checkRaw(b []byte) (Type, uint32, error) {
	var typ Type
	switch len(b) {
	case frameLen:
		typ = TypeClassic
	case fdFrameLen:
		typ = TypeFd
	default:
		return 0, 0, ErrFrameLen
	}

	rawID := binary.LittleEndian.Uint32(b[0:4])
	eff := rawID&canEFFFlag != 0
	rtr := rawID&canRTRFlag != 0
	isErr := rawID&canERRFlag != 0

	// Error frames carry no addressing mode: can_err_mask_t keeps bits 29-31
	// zero, so CAN_EFF_FLAG and CAN_RTR_FLAG are not valid with CAN_ERR_FLAG.
	if isErr && (eff || rtr) {
		return typ, 0, ErrBadFlags
	}
	// RTR has no CAN FD layout.
	if typ == TypeFd && rtr {
		return typ, 0, ErrBadFlags
	}
	// A standard frame fits 11 bits; an error frame carries a 29-bit error
	// class mask, so only non-error frames are checked against CAN_SFF_MASK.
	if !eff && !isErr && rawID&canEFFMask > canSFFMask {
		return typ, 0, ErrBadID
	}

	if typ == TypeClassic {
		if b[dlcOff] > maxDLC {
			return typ, 0, ErrBadDLC
		}
		// Byte 7 is len8_dlc in the modern kernel layout; DLC 9..15 is not
		// supported by this protocol, so it must be zero here.
		if b[padOff]|b[res0Off]|b[res1Off] != 0 {
			return typ, 0, ErrReserved
		}
		return typ, rawID, nil
	}

	if !validFDDataLen(b[dlcOff]) {
		return typ, 0, ErrBadLen
	}
	if b[fdFlagsOff]&^canFDMask != 0 {
		return typ, 0, ErrBadFlags
	}
	if b[res0Off]|b[res1Off] != 0 {
		return typ, 0, ErrReserved
	}
	return typ, rawID, nil
}
