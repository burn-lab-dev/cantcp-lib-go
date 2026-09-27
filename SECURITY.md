# Security

cantcp is a transport: it frames CAN frames in a byte stream and carries them
over plain TCP. Security properties of the channel are deliberately out of
scope and are the responsibility of the application and the infrastructure
that deploy the library.

Russian translation: [SECURITY.ru.md](SECURITY.ru.md).

## Threat model

The protocol assumes a **trusted perimeter**: a closed network segment (for
example an on-board or site network) in which every peer that can write to the
TCP stream is allowed to inject CAN frames. Work outside a trusted perimeter
without an external protection layer is not allowed.

## Authentication, encryption, replay

Authentication, channel encryption, integrity protection and replay
protection are **not** tasks of the library: they are the responsibility of
whoever applies it. If the stream leaves a trusted perimeter, the channel must
be protected by external means — TLS/mTLS, VPN, network segmentation — and the
parties must be authenticated at the application level. The frame format carries
no MAC, no nonce and no counter, so a peer that can write to the stream can
forge, modify, drop or replay any frame.

## CRC-8 is not cryptographic integrity

The CRC-8 is a framing sanity check: the polynomial and coverage are public and
about one in 256 random candidates passes it. It protects against accidental
corruption of the framing, not against a deliberate modification. An attacker
with write access to the stream recomputes the CRC trivially.

## Resynchronization

`Split` is self-synchronizing: after a corrupt candidate it may find a valid
frame even inside the data area of the corrupt one. This is intended, but with
write access to the stream it also lets an attacker inject phantom frames.
Inside a trusted perimeter this property only matters for robustness.

## CPU amplification

`Split` steps one byte forward on a CRC mismatch to keep the true header that
overlaps a false candidate. On adversarial input such as a repeating
`C3 3C 02` pattern a candidate is checked every three bytes with a full CRC
pass over 75 bytes, giving up to about ~25x CPU amplification per input byte
(~6x for classic packets). Inside a trusted perimeter this is acceptable; when
the stream may be hostile, bound the input on the server side (rate limit,
bytes per second) and treat `Stats().Dropped` growth as a hostile-stream
signal.

## Logging

`LevelTrace` writes the full hex of the raw frame to the log, **including the
payload** — machine telemetry and commands. This is sensitive data: tracing is
meant for debugging on an isolated bench only. Do not enable `LevelTrace` in
production, and treat traced logs as sensitive: keep them inside the trusted
perimeter and erase them together with the rest of the sensitive telemetry.

## Availability

The decoder reads from an `io.Reader` and does not set deadlines: a slow or
silent peer blocks the reading goroutine indefinitely. Setting `ReadDeadline`
(and an overall connection timeout) is the responsibility of the caller.

## Deployment checklist

- Keep the TCP port inside a trusted perimeter; never expose it to an
  untrusted network directly.
- If the stream leaves the perimeter, wrap it in TLS/mTLS or a VPN and
  authenticate peers at the application level.
- Do not enable `LevelTrace` in production; protect the logs.
- Set `ReadDeadline` and a connection timeout on every peer.
- When the input may be hostile, bound the rate and watch
  `Stats().Dropped` and `Stats().Skipped`.

## Reporting

Report a suspected vulnerability privately to the project owner instead of
opening a public issue; include the affected version, a reproduction and the
expected impact. Fixes are prepared privately and released together with the
advisory. Security notes for a protocol version are documented here.

The project is maintained by [BURN-LAB](https://burn-lab.ru); the contact
address is [stepanov.mikhail.y@gmail.com](mailto:stepanov.mikhail.y@gmail.com).
