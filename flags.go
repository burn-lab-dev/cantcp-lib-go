package cantcp

// Flag bits of a Frame. Combine the constants with bitwise OR and pass the
// result to Frame.SetFlags:
//
//	f.SetFlags(cantcp.FlagEFF | cantcp.FlagRTR)
//
// FlagEFF, FlagRTR and FlagERR come from the CAN identifier word; FlagBRS and
// FlagESI come from the CAN FD flags byte. The values are internal to the
// object: MarshalBinary and UnmarshalBinary map them to and from the raw
// SocketCAN bit positions.
const (
	FlagEFF uint8 = 1 << iota // extended (29-bit) identifier
	FlagRTR                   // remote transmission request (classic CAN only)
	FlagERR                   // error frame
	FlagBRS                   // CAN FD bit rate switch
	FlagESI                   // CAN FD error state indicator
)
