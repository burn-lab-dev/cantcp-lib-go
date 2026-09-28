// Package cantcp provides building blocks for CAN-over-TCP transport: framing
// of raw SocketCAN frames in a byte stream, plus encoding and decoding of
// Linux SocketCAN frames.
//
// The package carries frames only. Connection setup is plain TCP: handshakes,
// subscriptions, keepalives, statistics and health checks are not part of the
// protocol. A server exposes them over its own API (for example HTTP) and
// clients that need them poll that API.
//
// # Stream framing
//
// Split decodes a stream of packets:
//
//	[magic 2 bytes][type 1 byte][struct can_frame 16 | canfd_frame 72][CRC-8 1]
//
// The type byte is TypeClassic (0x01) or TypeFd (0x02) and selects the frame
// size. The frame layout follows Linux SocketCAN:
//
// can_frame (16 bytes):
//
//	can_id  uint32  (4 bytes, little-endian)
//	can_dlc uint8   (1 byte)
//	__pad   uint8   (1 byte, must be zero)
//	__res0  uint8   (1 byte, must be zero)
//	__res1  uint8   (1 byte, must be zero)
//	data    [8]byte
//
// canfd_frame (72 bytes):
//
//	can_id  uint32  (4 bytes, little-endian)
//	len     uint8   (1 byte)
//	flags   uint8   (1 byte: BRS 0x01, ESI 0x02; the kernel's FD marker
//	                 CANFD_FDF 0x04 is accepted on input and ignored)
//	__res0  uint8   (1 byte, must be zero)
//	__res1  uint8   (1 byte, must be zero)
//	data    [64]byte
//
// The reserved bytes take part in the CRC: the sender must write zeroes there,
// otherwise the CRC will not match.
//
// A parser is created only by New and configured only by the exported With
// functions; the parser type itself is not exported. Split returns the raw
// frame as the token: 16 bytes for classic CAN, 72 bytes for CAN FD. The token
// is a window into the bufio.Scanner buffer and is valid only until the next
// Scan call; use bytes.Clone to keep it. Encode builds a packet and selects
// the type by the frame length: 16 bytes for classic CAN, 72 bytes for CAN FD.
//
// # Frames
//
// Frame parses a raw frame into an identifier, flags and payload. The object
// is independent of the stream framing: UnmarshalBinary accepts 16 or 72 bytes
// and selects the layout by the length, MarshalBinary returns the raw frame,
// GetRaw returns a copy of the stored raw frame. Parsing is strict: bad length
// fields, non-zero reserved bytes, invalid flags and out-of-range identifiers
// are reported as sentinel errors.
//
// ValidateRaw is the same strict check without building a Frame: it reports
// the length-selected layout and the first error, with no copy and no
// allocation. ValidateRaw and UnmarshalBinary share the validation core, so
// both accept and reject exactly the same raw frames; the cantcp envelope
// (magic, packet type byte, CRC-8) belongs to Split and Decode only. An error
// frame carries no addressing mode: CAN_ERR_FLAG combined with CAN_EFF_FLAG or
// CAN_RTR_FLAG returns ErrBadFlags, while its 29-bit error class mask is
// accepted in ID.
//
// CAN FD data lengths are encoded in the 4-bit DLC field with a discrete
// scale, so only the following lengths exist:
//
//	DLC  0..8  9  10 11 12 13 14 15
//	len  0..8  12 16 20 24 32 48 64
//
// Any other length cannot be transmitted on a CAN FD bus and Linux
// can_fd_len2dlc rejects it too. Split stays tolerant and only rejects
// len > 64, so raw pass-through keeps working; the Frame model is strict and
// returns ErrBadLen for a length the DLC field cannot encode.
//
// # Codec
//
// The decoder and encoder returned by NewDecoder and NewEncoder wrap Split and
// Encode with an io.Reader / io.Writer API. Their types are not exported, like
// the parser of New: both are used through the constructor and the exported
// methods only.
//
//	d := cantcp.NewDecoder(conn)
//	for {
//		f, err := d.DecodeFrame()
//		if err != nil {
//			break
//		}
//		handle(f)
//	}
//	log.Printf("skipped=%d dropped=%d", d.Stats().Skipped, d.Stats().Dropped)
//
//	e := cantcp.NewEncoder(conn)
//	err := e.EncodeFrame(&f)
//
// DecodeFrameInto is the allocation-free form of DecodeFrame: it parses into a
// caller-owned Frame that is reused across calls.
//
//	d := cantcp.NewDecoder(conn)
//	var f cantcp.Frame
//	for {
//		if err := d.DecodeFrameInto(&f); err != nil {
//			break
//		}
//		handle(&f)
//	}
//
// Decode returns io.EOF at the end of the stream, ErrTruncated for a packet
// cut off in the middle and read errors unchanged. This lets a graceful
// shutdown tell a closed connection from a clean close and from a truncated
// packet:
//
//	for {
//		f, err := d.DecodeFrame()
//		switch {
//		case errors.Is(err, io.EOF): // the peer closed the stream
//			return nil
//		case errors.Is(err, net.ErrClosed): // our shutdown closed the conn
//			return ctx.Err()
//		case err != nil:
//			return err
//		}
//		handle(f)
//	}
//
// # Security
//
// The package is a transport and assumes a trusted perimeter: authentication,
// channel encryption, integrity protection and replay protection are the
// responsibility of the application that deploys it (TLS/mTLS, VPN, network
// segmentation). The CRC-8 is a framing sanity check, not cryptographic
// integrity. LevelTrace logs the raw frame including the payload, so it is a
// debugging tool for an isolated bench and must not be enabled in production.
// See SECURITY.md for the threat model and the deployment checklist.
//
// # Test vectors
//
// The canon of the protocol lives in github.com/burn-lab-dev/cantcp-spec:
// the specification and vectors.json with streams, raw frames, field values,
// counters and errors, so every implementation produces byte-for-byte
// identical results. testdata/vectors.json is a synced copy checked against
// the canon by scripts/sync_vectors.sh and by the CI.
//
// # Errors
//
// Every error is a sentinel compared with errors.Is: ErrFrameLen, ErrBadDLC,
// ErrBadLen, ErrBadType, ErrBadFlags, ErrBadID, ErrReserved and ErrTruncated.
// The package does not use fmt: error texts are static.
//
// The CRC-8 is a framing sanity check, not cryptographic integrity: about one
// in 256 random candidates passes it.
//
// The library is developed by BURN-LAB (https://burn-lab.ru): embedded
// software development — Linux, drivers, CAN and industrial telemetry.
package cantcp
