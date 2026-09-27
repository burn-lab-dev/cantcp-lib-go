package cantcp

import (
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
)

// Frame layout constants from Linux SocketCAN struct can_frame and
// struct canfd_frame.
const (
	frameLen   = 16 // sizeof(struct can_frame)
	fdFrameLen = 72 // sizeof(struct canfd_frame)
	dlcOff     = 4  // can_dlc (classic) / len (CAN FD) offset
	padOff     = 5  // classic __pad, must be zero
	fdFlagsOff = 5  // canfd_frame flags offset
	res0Off    = 6  // __res0, must be zero in both layouts
	res1Off    = 7  // classic len8_dlc / canfd __res1, must be zero
	dataOff    = 8  // data[] offset in both structs
	maxDLC     = 8  // CAN_MAX_DLEN

	// maxFDDataLen is the CANFD_MAX_DLEN; valid CAN FD data lengths are the
	// discrete set accepted by validFDDataLen (the 4-bit DLC encoding).
	maxFDDataLen = 64
)

// can_id flag bits and identifier masks from Linux SocketCAN.
const (
	canEFFFlag = 0x80000000 // CAN_EFF_FLAG
	canRTRFlag = 0x40000000 // CAN_RTR_FLAG
	canERRFlag = 0x20000000 // CAN_ERR_FLAG
	canEFFMask = 0x1FFFFFFF // CAN_EFF_MASK
	canSFFMask = 0x000007FF // CAN_SFF_MASK
)

// canfd_frame flags byte bits from Linux SocketCAN.
const (
	canFDBRS  = 0x01 // CANFD_BRS
	canFDESI  = 0x02 // CANFD_ESI
	canFDMask = canFDBRS | canFDESI
)

// validFDDataLen reports whether n is a CAN FD data length that can be encoded
// in the 4-bit DLC field: 0..8, 12, 16, 20, 24, 32, 48, 64.
func validFDDataLen(n byte) bool {
	switch n {
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 12, 16, 20, 24, 32, 48, 64:
		return true
	default:
		return false
	}
}

// Type selects the frame layout: classic CAN (can_frame, 16 bytes) or CAN FD
// (canfd_frame, 72 bytes). The same values are used as the packet type byte of
// the cantcp stream framing.
type Type uint8

const (
	TypeClassic Type = 0x01 // can_frame, 16 bytes
	TypeFd      Type = 0x02 // canfd_frame, 72 bytes
)

// String returns "CAN", "CAN FD" or "unknown".
func (t Type) String() string {
	switch t {
	case TypeClassic:
		return "CAN"
	case TypeFd:
		return "CAN FD"
	default:
		return "unknown"
	}
}

// Frame is a parsed CAN or CAN FD frame with the identifier and the flags
// split into separate fields. It is independent of the cantcp stream framing:
// MarshalBinary and UnmarshalBinary work with the raw Linux SocketCAN layouts
// (struct can_frame, 16 bytes, and struct canfd_frame, 72 bytes) only.
//
// Data is the payload; len(Data) is the frame length field (can_dlc or len).
// The raw frame is kept internally and returned by GetRaw. Copying a Frame
// value copies the fields, but Data keeps pointing into the raw storage of the
// original frame: use GetRaw for an independent copy.
//
// FlagERR is allowed for both layouts and carries a 29-bit error class mask in
// ID, so an error frame is never extended or remote: FlagERR combined with
// FlagEFF or FlagRTR returns ErrBadFlags. FlagRTR is not valid for CAN FD.
type Frame struct {
	ID   uint32
	Type Type
	EFF  bool
	RTR  bool
	ERR  bool
	BRS  bool
	ESI  bool
	Data []byte
	raw  [fdFrameLen]byte
}

// UnmarshalBinary parses a raw frame into f: 16 bytes are decoded as a classic
// can_frame, 72 bytes as a canfd_frame, any other length returns ErrFrameLen.
//
// Parsing is strict and goes through the same validation as ValidateRaw:
// can_dlc > 8 returns ErrBadDLC, an invalid canfd len (greater than 64 or not
// encodable in the 4-bit DLC field) returns ErrBadLen, non-zero padding and
// reserved bytes return ErrReserved, FlagRTR and unknown bits of the CAN FD
// flags byte return ErrBadFlags, an identifier that does not fit the
// addressing mode returns ErrBadID (more than 11 bits without FlagEFF, more
// than 29 bits with it, no silent masking), and FlagEFF or FlagRTR combined
// with FlagERR returns ErrBadFlags. On success Data is a window into the
// internal raw frame, no copy is made, and f is not modified when an error is
// returned.
//
// Bytes of the data area beyond the length field are not part of Data and are
// not preserved: MarshalBinary writes zeroes there.
func (f *Frame) UnmarshalBinary(b []byte) error {
	typ, rawID, err := checkRaw(b)
	if err != nil {
		return err
	}

	eff := rawID&canEFFFlag != 0
	rtr := rawID&canRTRFlag != 0
	isErr := rawID&canERRFlag != 0
	// An error frame carries a 29-bit error class mask in ID, so the 11-bit
	// address mask does not apply to it.
	var id uint32
	if eff || isErr {
		id = rawID & canEFFMask
	} else {
		id = rawID & canSFFMask
	}
	var brs, esi bool
	if typ == TypeFd {
		brs = b[fdFlagsOff]&canFDBRS != 0
		esi = b[fdFlagsOff]&canFDESI != 0
	}

	// Validation passed: commit everything at once.
	f.Type = typ
	f.raw = [fdFrameLen]byte{}
	copy(f.raw[:], b)
	f.ID = id
	f.EFF = eff
	f.RTR = rtr
	f.ERR = isErr
	f.BRS = brs
	f.ESI = esi
	f.Data = f.raw[dataOff : dataOff+int(b[dlcOff])]
	return nil
}

// MarshalBinary builds the raw frame: a 16-byte can_frame for TypeClassic or a
// 72-byte canfd_frame for TypeFd. It validates Type, ID, flags and the data
// length, stores the result in the internal raw frame and returns a copy of
// it; the data area beyond len(Data) is zeroed. An unset or unknown Type
// returns ErrBadType, an identifier that does not fit the addressing mode
// returns ErrBadID (29 bits for an error frame, 11 bits for a standard frame,
// 29 with FlagEFF), flags that are not valid for the type return ErrBadFlags
// (FlagERR with FlagEFF or FlagRTR, FlagRTR in CAN FD, FlagBRS and FlagESI in
// classic CAN), and an invalid Data length returns ErrBadDLC or ErrBadLen (for
// CAN FD the length must be encodable in the 4-bit DLC field).
func (f *Frame) MarshalBinary() ([]byte, error) {
	var n int
	switch f.Type {
	case TypeClassic:
		n = frameLen
		if len(f.Data) > maxDLC {
			return nil, ErrBadDLC
		}
		if f.BRS || f.ESI {
			return nil, ErrBadFlags
		}
	case TypeFd:
		n = fdFrameLen
		if len(f.Data) > maxFDDataLen || !validFDDataLen(byte(len(f.Data))) {
			return nil, ErrBadLen
		}
		if f.RTR {
			return nil, ErrBadFlags
		}
	default:
		return nil, ErrBadType
	}
	// An error frame carries no addressing mode: can_err_mask_t keeps bits
	// 29-31 zero, so FlagERR excludes FlagEFF and FlagRTR.
	if f.ERR && (f.EFF || f.RTR) {
		return nil, ErrBadFlags
	}
	limit := uint32(canSFFMask)
	if f.EFF || f.ERR {
		limit = canEFFMask
	}
	if f.ID > limit {
		return nil, ErrBadID
	}

	rawID := f.ID
	if f.EFF {
		rawID |= canEFFFlag
	}
	if f.RTR {
		rawID |= canRTRFlag
	}
	if f.ERR {
		rawID |= canERRFlag
	}
	clear(f.raw[:dataOff])
	binary.LittleEndian.PutUint32(f.raw[0:4], rawID)
	f.raw[dlcOff] = byte(len(f.Data))
	if f.Type == TypeFd {
		if f.BRS {
			f.raw[fdFlagsOff] |= canFDBRS
		}
		if f.ESI {
			f.raw[fdFlagsOff] |= canFDESI
		}
	}
	clear(f.raw[dataOff+len(f.Data):])
	copy(f.raw[dataOff:], f.Data)
	f.Data = f.raw[dataOff : dataOff+len(f.Data)]
	out := make([]byte, n)
	copy(out, f.raw[:n])
	return out, nil
}

// SetFlags replaces all flags of the frame with the given bit set. Build the
// set from the Flag constants, for example:
//
//	err := f.SetFlags(cantcp.FlagEFF | cantcp.FlagRTR)
//
// Flags that are unknown or not valid for the current Type return
// ErrBadFlags: FlagBRS and FlagESI are valid for CAN FD only, FlagRTR is not
// valid for CAN FD, and an error frame carries no addressing mode, so FlagERR
// excludes FlagEFF and FlagRTR. An unset or unknown Type returns ErrBadType.
func (f *Frame) SetFlags(flags uint8) error {
	var mask uint8
	switch f.Type {
	case TypeClassic:
		mask = FlagEFF | FlagRTR | FlagERR
	case TypeFd:
		mask = FlagEFF | FlagERR | FlagBRS | FlagESI
	default:
		return ErrBadType
	}
	if flags&^mask != 0 {
		return ErrBadFlags
	}
	if flags&FlagERR != 0 && flags&(FlagEFF|FlagRTR) != 0 {
		return ErrBadFlags
	}
	f.EFF = flags&FlagEFF != 0
	f.RTR = flags&FlagRTR != 0
	f.ERR = flags&FlagERR != 0
	f.BRS = flags&FlagBRS != 0
	f.ESI = flags&FlagESI != 0
	return nil
}

// GetRaw returns a copy of the stored raw frame: 16 bytes for TypeClassic,
// 72 bytes for TypeFd. An unset or unknown Type returns nil.
func (f *Frame) GetRaw() []byte {
	var n int
	switch f.Type {
	case TypeClassic:
		n = frameLen
	case TypeFd:
		n = fdFrameLen
	default:
		return nil
	}
	out := make([]byte, n)
	copy(out, f.raw[:n])
	return out
}

// String returns a human-readable single-line representation, for example:
//
//	Frame{Type:CAN, ID:0x123, Flags:EFF|RTR, DLC:2, Data:0a0b}
//	Frame{Type:CAN FD, ID:0x1abc, Flags:BRS, Len:64, Data:...}
func (f Frame) String() string {
	var b strings.Builder
	b.WriteString("Frame{Type:")
	b.WriteString(f.Type.String())
	b.WriteString(", ID:0x")
	b.WriteString(strconv.FormatUint(uint64(f.ID), 16))
	b.WriteString(", Flags:")
	b.WriteString(f.flagsString())
	if f.Type == TypeFd {
		b.WriteString(", Len:")
	} else {
		b.WriteString(", DLC:")
	}
	b.WriteString(strconv.Itoa(len(f.Data)))
	b.WriteString(", Data:")
	b.WriteString(hex.EncodeToString(f.Data))
	b.WriteString("}")
	return b.String()
}

// flagsString returns the set flags joined with "|", or "none".
func (f Frame) flagsString() string {
	var b strings.Builder
	write := func(name string, set bool) {
		if !set {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('|')
		}
		b.WriteString(name)
	}
	write("EFF", f.EFF)
	write("RTR", f.RTR)
	write("ERR", f.ERR)
	write("BRS", f.BRS)
	write("ESI", f.ESI)
	if b.Len() == 0 {
		return "none"
	}
	return b.String()
}
