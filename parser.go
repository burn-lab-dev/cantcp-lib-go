package cantcp

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"math"
	"slices"
)

// Packet layout constants.
const (
	magicLen = 2 // packet header
	typeLen  = 1 // packet type byte
	crcLen   = 1 // CRC-8
)

// Packet offsets and total lengths.
const (
	typeOff       = magicLen           // packet type offset
	frameOff      = magicLen + typeLen // raw frame offset
	classicPktLen = frameOff + frameLen + crcLen
	fdPktLen      = frameOff + fdFrameLen + crcLen
)

// BadFramePolicy selects how Split reacts to a structurally invalid packet:
// an unknown packet type byte, can_dlc > 8 or canfd len > 64.
type BadFramePolicy int

const (
	// BadFrameSkip drops the invalid packet, counts it and keeps parsing.
	BadFrameSkip BadFramePolicy = iota
	// BadFrameFail stops parsing and returns ErrBadType, ErrBadDLC or ErrBadLen.
	BadFrameFail
)

// Stats reports parser counters since the last ResetStats call.
type Stats struct {
	Skipped   int // bytes discarded while resynchronizing
	Dropped   int // candidates rejected by CRC
	BadType   int // packets with an unknown type byte
	BadDLC    int // classic frames with can_dlc > 8
	BadLen    int // CAN FD frames with canfd len > 64
	Truncated int // bytes lost to a packet cut off at EOF
}

// addStat adds n to a counter, saturating at math.MaxInt instead of
// overflowing: a garbage stream can drive the counters without bound, so a
// 32-bit build would wrap around otherwise.
func addStat(dst *int, n int) {
	if n > math.MaxInt-*dst {
		*dst = math.MaxInt
		return
	}
	*dst += n
}

// parser decodes a byte stream of framed CAN packets:
//
//	[magic 2 bytes][type 1 byte][struct can_frame 16 | canfd_frame 72][CRC-8 1]
//
// The type byte is TypeClassic or TypeFd and selects the frame size. parser
// keeps its own configuration, its own CRC-8 lookup table and stream
// counters. It is not safe for concurrent use: use one parser per stream.
//
// A parser is created only by New and configured only by the exported With
// functions; the type itself is not exported.
type parser struct {
	magic       [magicLen]byte
	crc         crc8
	coverHeader bool
	badPolicy   BadFramePolicy
	log         Logger
	logLevel    slog.Level
	stats       Stats
}

// New returns a parser with default configuration: magic 0xC3 0x3C, CRC-8
// polynomial 0x07 covering magic+type+frame, BadFrameSkip policy and logging
// disabled at level Info. Options replace individual defaults.
func New(opts ...option) *parser {
	p := &parser{
		magic:       [magicLen]byte{0xC3, 0x3C},
		crc:         newCRC8(0x07),
		coverHeader: true,
		badPolicy:   BadFrameSkip,
		logLevel:    slog.LevelInfo,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Split is a bufio.SplitFunc for streams of framed CAN and CAN FD packets:
//
//	[magic 2 bytes][type 1 byte][struct can_frame 16 | canfd_frame 72][CRC-8 1]
//
// Split discards garbage, resynchronizes on CRC mismatches and returns the raw
// frame as the token: 16 bytes for TypeClassic, 72 bytes for TypeFd. The token
// is a window into the Scanner buffer and is valid only until the next Scan
// call; use bytes.Clone to keep it.
//
// A structurally invalid packet follows the configured BadFramePolicy: an
// unknown type byte, can_dlc > 8 or canfd len > 64 is counted and skipped, or
// returned as ErrBadType, ErrBadDLC or ErrBadLen. A packet cut off at EOF
// returns ErrTruncated.
//
// Resynchronization may find a valid frame inside the data area of a corrupt
// candidate: this is intended for a self-synchronizing protocol.
func (p *parser) Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for {
		// 1. Skip garbage up to the first magic byte.
		if len(data) > 0 && data[0] != p.magic[0] {
			i := bytes.IndexByte(data, p.magic[0])
			if i < 0 {
				addStat(&p.stats.Skipped, len(data))
				p.traceSkip(len(data))
				return advance + len(data), nil, nil
			}
			addStat(&p.stats.Skipped, i)
			p.traceSkip(i)
			advance += i
			data = data[i:]
		}

		// 2. Match the second magic byte.
		if len(data) < magicLen {
			if !atEOF || len(data) == 0 {
				return advance, nil, nil
			}
			addStat(&p.stats.Truncated, len(data))
			p.warnTruncated(len(data))
			return advance + len(data), nil, ErrTruncated
		}
		if data[1] != p.magic[1] {
			addStat(&p.stats.Skipped, 1)
			advance++
			data = data[1:]
			continue
		}

		// 3. Wait for the type byte.
		if len(data) < frameOff {
			if !atEOF {
				return advance, nil, nil
			}
			addStat(&p.stats.Truncated, len(data))
			p.warnTruncated(len(data))
			return advance + len(data), nil, ErrTruncated
		}

		// 4. Validate the packet type and pick the frame layout.
		var frameSize, pktLen int
		switch Type(data[typeOff]) {
		case TypeClassic:
			frameSize, pktLen = frameLen, classicPktLen
		case TypeFd:
			frameSize, pktLen = fdFrameLen, fdPktLen
		default:
			addStat(&p.stats.BadType, 1)
			p.warnBadType(data[typeOff])
			if p.badPolicy == BadFrameFail {
				return advance, nil, ErrBadType
			}
			addStat(&p.stats.Skipped, 1)
			advance++
			data = data[1:]
			continue
		}

		// 5. Wait for the whole packet.
		if len(data) < pktLen {
			if !atEOF {
				return advance, nil, nil
			}
			addStat(&p.stats.Truncated, len(data))
			p.warnTruncated(len(data))
			return advance + len(data), nil, ErrTruncated
		}

		pkt := data[:pktLen]

		// 6. Verify CRC. A mismatch steps one byte forward so that a real
		// header overlapping the false candidate is not lost.
		if p.crc.sum(p.crcSpan(pkt, pktLen)) != pkt[pktLen-1] {
			addStat(&p.stats.Dropped, 1)
			addStat(&p.stats.Skipped, 1)
			p.traceMismatch(p.stats.Dropped)
			advance++
			data = data[1:]
			continue
		}

		// 7. Validate the frame length field.
		frame := pkt[frameOff : frameOff+frameSize]
		var limit byte = maxDLC
		badErr := ErrBadDLC
		if frameSize == fdFrameLen {
			limit, badErr = maxFDDataLen, ErrBadLen
		}
		if length := frame[dlcOff]; length > limit {
			if frameSize == frameLen {
				addStat(&p.stats.BadDLC, 1)
				p.warnBadDLC(length)
			} else {
				addStat(&p.stats.BadLen, 1)
				p.warnBadLen(length)
			}
			if p.badPolicy == BadFrameFail {
				return advance, nil, badErr
			}
			addStat(&p.stats.Skipped, 1)
			advance++
			data = data[1:]
			continue
		}

		p.traceFrame(data[typeOff], frame)
		return advance + pktLen, frame, nil
	}
}

// Encode appends a packet carrying the raw frame to dst:
//
//	[magic 2 bytes][type 1 byte][frame 16 | 72 bytes][CRC-8 1 byte]
//
// The frame length selects the packet type: 16 bytes build a classic packet
// (TypeClassic), 72 bytes a CAN FD packet (TypeFd). Encode uses the same CRC
// table and coverage as Split, so a packet built by Encode is always accepted
// by Split of the same parser. A frame of any other length returns
// ErrFrameLen; can_dlc > 8 returns ErrBadDLC and canfd len > 64 returns
// ErrBadLen. dst is left unchanged in all error cases.
func (p *parser) Encode(dst, frame []byte) ([]byte, error) {
	var typ Type
	switch len(frame) {
	case frameLen:
		if frame[dlcOff] > maxDLC {
			return dst, ErrBadDLC
		}
		typ = TypeClassic
	case fdFrameLen:
		if frame[dlcOff] > maxFDDataLen {
			return dst, ErrBadLen
		}
		typ = TypeFd
	default:
		return dst, ErrFrameLen
	}
	start := len(dst)
	pktLen := frameOff + len(frame) + crcLen
	dst = slices.Grow(dst, pktLen)
	dst = append(dst, p.magic[0], p.magic[1], byte(typ))
	dst = append(dst, frame...)
	return append(dst, p.crc.sum(p.crcSpan(dst[start:], pktLen))), nil
}

// Stats returns a copy of the stream counters.
func (p *parser) Stats() Stats {
	return p.stats
}

// ResetStats zeroes the stream counters.
func (p *parser) ResetStats() {
	p.stats = Stats{}
}

// crcSpan returns the part of the packet covered by the CRC. pkt holds at
// least pktLen-crcLen bytes of the packet: the CRC byte itself may be absent.
func (p *parser) crcSpan(pkt []byte, pktLen int) []byte {
	if p.coverHeader {
		return pkt[:pktLen-crcLen]
	}
	return pkt[frameOff : pktLen-crcLen]
}

// enabled reports whether a message at level would be logged.
func (p *parser) enabled(level slog.Level) bool {
	return p.log != nil && level >= p.logLevel
}

// traceSkip traces discarded bytes.
func (p *parser) traceSkip(n int) {
	if !p.enabled(LevelTrace) {
		return
	}
	p.log.Log(context.Background(), LevelTrace, "garbage skipped", "bytes", n)
}

// traceMismatch traces a candidate rejected by CRC; candidate is its sequence
// number in the stream (Stats.Dropped).
func (p *parser) traceMismatch(candidate int) {
	if !p.enabled(LevelTrace) {
		return
	}
	p.log.Log(context.Background(), LevelTrace, "crc mismatch", "candidate", candidate)
}

// warnTruncated warns about bytes lost to a packet cut off at EOF.
func (p *parser) warnTruncated(n int) {
	if !p.enabled(slog.LevelWarn) {
		return
	}
	p.log.Log(context.Background(), slog.LevelWarn, "stream truncated", "bytes", n)
}

// warnBadType warns about an unknown packet type byte.
func (p *parser) warnBadType(t byte) {
	if !p.enabled(slog.LevelWarn) {
		return
	}
	p.log.Log(context.Background(), slog.LevelWarn, "unknown packet type", "type", t)
}

// warnBadDLC warns about can_dlc > 8.
func (p *parser) warnBadDLC(dlc byte) {
	if !p.enabled(slog.LevelWarn) {
		return
	}
	p.log.Log(context.Background(), slog.LevelWarn, "can_dlc > 8", "dlc", dlc)
}

// warnBadLen warns about canfd len > 64.
func (p *parser) warnBadLen(length byte) {
	if !p.enabled(slog.LevelWarn) {
		return
	}
	p.log.Log(context.Background(), slog.LevelWarn, "canfd len > 64", "len", length)
}

// traceFrame traces a successfully parsed frame.
func (p *parser) traceFrame(typ byte, frame []byte) {
	if !p.enabled(LevelTrace) {
		return
	}
	p.log.Log(context.Background(), LevelTrace, "frame parsed",
		"type", typ,
		"length", frame[dlcOff],
		"frame", hex.EncodeToString(frame))
}
