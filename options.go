package cantcp

import "log/slog"

// option configures a parser. Options are applied in order by New. The type is
// not exported: a parser is configured only by the exported With functions.
type option func(*parser)

// WithMagic sets the two-byte packet header, replacing the default
// 0xC3 0x3C. The magic is always exactly two bytes.
func WithMagic(hi, lo byte) option {
	return func(p *parser) { p.magic = [magicLen]byte{hi, lo} }
}

// WithCRCPoly sets the CRC-8 polynomial (default 0x07) and rebuilds the
// lookup table of the parser.
func WithCRCPoly(poly byte) option {
	return func(p *parser) { p.crc = newCRC8(poly) }
}

// WithCRCCoverFrameOnly makes the CRC cover the raw frame only instead of
// magic+type+frame (the default).
func WithCRCCoverFrameOnly() option {
	return func(p *parser) { p.coverHeader = false }
}

// WithBadFramePolicy selects how Split reacts to an unknown packet type byte
// or a bad frame length field (can_dlc > 8, canfd len > 64).
func WithBadFramePolicy(policy BadFramePolicy) option {
	return func(p *parser) { p.badPolicy = policy }
}

// WithLogger sets the logger. A nil logger disables logging.
func WithLogger(l Logger) option {
	return func(p *parser) { p.log = l }
}

// WithLogLevel sets the minimum level to log (default slog.LevelInfo).
// Use LevelTrace to trace every parsed frame, garbage skip, CRC mismatch and
// rejected packet type.
//
// Warning: LevelTrace writes the full hex of the raw frame to the log,
// including the payload (machine telemetry and commands). It is a debugging
// tool for an isolated bench only: do not enable it in production, and treat
// traced logs as sensitive data. See SECURITY.md.
func WithLogLevel(level slog.Level) option {
	return func(p *parser) { p.logLevel = level }
}
