# cantcp-lib-go

[![CI](https://github.com/burn-lab-dev/cantcp-lib-go/actions/workflows/ci.yml/badge.svg)](https://github.com/burn-lab-dev/cantcp-lib-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/burn-lab-dev/cantcp-lib-go.svg)](https://pkg.go.dev/github.com/burn-lab-dev/cantcp-lib-go)
[![Go version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](go.mod)

Go library for the **cantcp** protocol: encoding and decoding of Linux
SocketCAN frames, stream framing with CRC-8, plus a parsed CAN / CAN FD frame
model.

> **Status: WIP.** The API is not stable, the protocol is in development (`v0`).

Russian documentation: [README.ru.md](README.ru.md).

Developed by **[BURN-LAB](https://burn-lab.ru)** — embedded software
development: Linux, drivers, CAN and industrial telemetry.

## Scope

The package carries CAN frames only: stream framing, frame parsing and the
codec. Connection setup is plain TCP: handshakes, subscriptions, keepalives,
statistics and health checks are not part of the protocol. A server exposes
them over its own API (for example HTTP) and clients poll that API when they
need them.

## Stream framing

`Split` decodes a byte stream of packets:

```
[magic 2 bytes][type 1 byte][can_frame 16 | canfd_frame 72][CRC-8 1 byte]
```

The packet type byte selects the frame layout:

| Value | Constant | Frame | Size |
|---|---|---|---|
| `0x01` | `TypeClassic` | `struct can_frame` | 16 |
| `0x02` | `TypeFd` | `struct canfd_frame` | 72 |

Classic frame (`can_frame`):

| Offset | Size | Field |
|---|---|---|
| 0 | 4 | `can_id` (little-endian) |
| 4 | 1 | `can_dlc` |
| 5 | 1 | `__pad` (must be zero) |
| 6 | 1 | `__res0` (must be zero) |
| 7 | 1 | `__res1` (must be zero) |
| 8 | 8 | `data` |

CAN FD frame (`canfd_frame`):

| Offset | Size | Field |
|---|---|---|
| 0 | 4 | `can_id` (little-endian) |
| 4 | 1 | `len` |
| 5 | 1 | `flags` (`BRS` 0x01, `ESI` 0x02; the kernel's `CANFD_FDF` 0x04 is accepted on input and ignored) |
| 6 | 1 | `__res0` (must be zero) |
| 7 | 1 | `__res1` (must be zero) |
| 8 | 64 | `data` |

The reserved bytes take part in the CRC: the sender must write zeroes there.
By default the CRC covers `magic+type+frame`, `WithCRCCoverFrameOnly` makes it
cover the raw frame only.

`Split` returns the raw frame as the token: 16 bytes for classic CAN, 72 bytes
for CAN FD. The token is a window into the `bufio.Scanner` buffer and is valid
only until the next `Scan` call; use `bytes.Clone` to keep it.

## Quick start

Reading with a decoder:

```go
d := cantcp.NewDecoder(conn,
	cantcp.WithLogger(slog.Default()),
	cantcp.WithLogLevel(cantcp.LevelTrace),
)
for {
	f, err := d.DecodeFrame()
	switch {
	case errors.Is(err, io.EOF): // the peer closed the stream
		return
	case errors.Is(err, net.ErrClosed): // shutdown: we closed the connection
		return
	case err != nil:
		log.Println("decode:", err) // ErrTruncated, ErrBadType, ...
		return
	}
	use(f)
}
log.Printf("skipped=%d dropped=%d", d.Stats().Skipped, d.Stats().Dropped)
```

Writing with an encoder:

```go
e := cantcp.NewEncoder(conn)
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
if err := e.EncodeFrame(&f); err != nil {
	log.Println("encode:", err)
}
```

Low-level `Split` / `Encode` stay available for custom readers and writers;
see the API reference below.

# API reference

The package API is built from three kinds of objects and plain functions:

- `New` returns a **parser** (the type is not exported), configured by the
  `With...` option functions and used through its exported methods;
- `NewDecoder` and `NewEncoder` return a **decoder** and an **encoder** (their
  types are not exported either), used through their exported methods;
- `Frame` is the exported parsed-frame object; `Type`, `Stats`,
  `BadFramePolicy`, `Logger`, the `Flag...` constants and the `Err...`
  sentinels complete the surface;
- plain functions: `ValidateRaw`.

## `New` — the parser

```go
func New(opts ...option) *parser
```

Creates the parser; the type itself is not exported, so a parser is obtained
only through `New` and configured only through the exported `With` functions.
Defaults: magic `0xC3 0x3C`, CRC-8 polynomial `0x07` covering
`magic+type+frame`, `BadFrameSkip` policy, logging disabled at `slog.LevelInfo`.

```go
p := cantcp.New(cantcp.WithLogLevel(cantcp.LevelTrace))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
advance, _, _ := p.Split(pkt, true) // 20
```

### Parser settings

The option functions below configure `New`; the same options configure
`NewDecoder` and `NewEncoder`, so a packet written with one option set is
always accepted by a reader created with the same set.

| Option | Default | Description |
|---|---|---|
| `WithMagic(hi, lo byte)` | `0xC3 0x3C` | Two-byte packet header |
| `WithCRCPoly(poly byte)` | `0x07` (SMBus) | CRC-8 polynomial, rebuilds the table |
| `WithCRCCoverFrameOnly()` | off | CRC covers the raw frame only |
| `WithBadFramePolicy(policy)` | `BadFrameSkip` | Reaction to an invalid packet |
| `WithLogger(l Logger)` | `nil` | Logger; `nil` disables logging |
| `WithLogLevel(level slog.Level)` | `slog.LevelInfo` | Minimum level to log |

#### `WithMagic(hi, lo byte)`

```go
func WithMagic(hi, lo byte) option
```

Sets the two-byte packet header, replacing the default `0xC3 0x3C`. The magic
is always exactly two bytes.

```go
p := cantcp.New(cantcp.WithMagic(0x11, 0x22))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
fmt.Printf("%x\n", pkt[:2]) // 1122
```

#### `WithCRCPoly(poly byte)`

```go
func WithCRCPoly(poly byte) option
```

Sets the CRC-8 polynomial (default `0x07`) and rebuilds the lookup table of the
parser.

```go
p := cantcp.New(cantcp.WithCRCPoly(0x1D))
frame := make([]byte, 16)
pkt, _ := p.Encode(nil, frame)
advance, token, _ := p.Split(pkt, true) // 20, token == frame
```

#### `WithCRCCoverFrameOnly()`

```go
func WithCRCCoverFrameOnly() option
```

Makes the CRC cover the raw frame only instead of `magic+type+frame` (the
default).

```go
p := cantcp.New(cantcp.WithCRCCoverFrameOnly())
frame := make([]byte, 72)
pkt, _ := p.Encode(nil, frame)
advance, token, _ := p.Split(pkt, true) // 76, token == frame
```

#### `WithBadFramePolicy(policy)`

```go
func WithBadFramePolicy(policy BadFramePolicy) option
```

Selects how `Split` reacts to an unknown packet type byte or a bad frame length
field (`can_dlc > 8`, `canfd len > 64`); see `BadFramePolicy`.

```go
p := cantcp.New(cantcp.WithBadFramePolicy(cantcp.BadFrameFail))
_, _, err := p.Split([]byte{0xC3, 0x3C, 0x7F}, false)
fmt.Println(err) // cantcp: unknown frame type
```

#### `WithLogger(l Logger)`

```go
func WithLogger(l Logger) option
```

Sets the logger. A `nil` logger (the default) disables logging. `*slog.Logger`
implements `Logger` directly.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()))
```

#### `WithLogLevel(level slog.Level)`

```go
func WithLogLevel(level slog.Level) option
```

Sets the minimum level to log (default `slog.LevelInfo`). Use `LevelTrace` to
trace every parsed frame, garbage skip, CRC mismatch and rejected packet type.

> **Warning:** `LevelTrace` writes the full hex of the raw frame to the log,
> **including the payload** — machine telemetry and commands. This is sensitive
> data: tracing is meant for debugging on an isolated bench only. Do not enable
> `LevelTrace` in production, and treat traced logs as sensitive.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()), cantcp.WithLogLevel(slog.LevelWarn))
// per-frame tracing is off, only warnings are logged
```

### Parser methods

#### `Split`

```go
func (p *parser) Split(data []byte, atEOF bool) (advance int, token []byte, err error)
```

A `bufio.SplitFunc` for streams of classic and CAN FD packets. Discards
garbage, resynchronizes on CRC mismatches and returns the raw frame (`16` or
`72` bytes by the type byte). `BadFrameSkip` counts and drops an unknown type
byte, `can_dlc > 8` or `canfd len > 64`; `BadFrameFail` returns `ErrBadType`,
`ErrBadDLC` or `ErrBadLen`. A packet cut off at EOF returns `ErrTruncated`.

```go
sc := bufio.NewScanner(conn)
sc.Split(p.Split)
for sc.Scan() {
	raw := bytes.Clone(sc.Bytes()) // 16 or 72 bytes, an independent copy
	handleRaw(raw)
}
```

#### `Encode`

```go
func (p *parser) Encode(dst, frame []byte) ([]byte, error)
```

Appends a packet carrying the raw frame to `dst`. The frame length selects the
type: `16` bytes build a `TypeClassic` packet, `72` bytes a `TypeFd` packet.
Any other length returns `ErrFrameLen`; `can_dlc > 8` returns `ErrBadDLC`,
`canfd len > 64` returns `ErrBadLen`. `dst` is unchanged on error.

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
raw, _ := f.MarshalBinary()
pkt, _ := p.Encode(nil, raw) // 76 bytes
```

#### `Stats`, `ResetStats`

```go
func (p *parser) Stats() Stats
func (p *parser) ResetStats()
```

`Stats` returns a copy of the counters since the last reset, `ResetStats`
zeroes them. See `Stats` for the counter fields.

```go
_, _, _ = p.Split([]byte{0x01, 0x02}, false)
fmt.Println("skipped:", p.Stats().Skipped) // skipped: 2
p.ResetStats()
fmt.Println("after reset:", p.Stats().Skipped) // after reset: 0
```

## `NewDecoder` — the decoder

```go
func NewDecoder(r io.Reader, opts ...option) *decoder
```

Returns a decoder reading packets from any `io.Reader`. The returned type is
not exported, like the parser of `New`: a decoder is used through the returned
value and its exported methods only. Options configure it exactly like `New`.
The decoder is not safe for concurrent use: use one decoder per stream.

```go
d := cantcp.NewDecoder(conn, cantcp.WithLogger(slog.Default()))
```

### Decoder methods

#### `Decode`

```go
func (d *decoder) Decode() ([]byte, error)
```

Returns the next raw frame: `16` bytes for classic CAN, `72` bytes for CAN FD.
The slice is an independent copy and stays valid after the next call.

The end of the stream returns `io.EOF`; a packet cut off in the middle returns
`ErrTruncated`; read errors are returned unchanged. A closed `net.Conn`
surfaces as `net.ErrClosed`, so a graceful shutdown can be told apart both from
a clean close (`io.EOF`) and from a truncated packet (`ErrTruncated`).

```go
d := cantcp.NewDecoder(conn)
for {
	raw, err := d.Decode()
	switch {
	case errors.Is(err, io.EOF):
		return
	case err != nil:
		log.Println("decode:", err)
		return
	}
	handleRaw(raw)
}
```

#### `DecodeFrame`

```go
func (d *decoder) DecodeFrame() (Frame, error)
```

Returns the next frame parsed into a `Frame`. It is a convenience wrapper
around `DecodeFrameInto`: use `DecodeFrameInto` to parse into a caller-owned
`Frame` and make no per-frame allocation.

The splitter stays tolerant (`canfd len` up to 64) while the `Frame` model is
strict, so a `canfd len` that the 4-bit DLC field cannot encode is returned as
`ErrBadLen` even though the splitter accepted it.

The returned frame is independent of the decoder: it keeps its own raw frame,
so `Data` stays valid after the next call.

```go
d := cantcp.NewDecoder(conn)
for {
	f, err := d.DecodeFrame()
	if err != nil {
		break
	}
	use(f)
}
```

#### `DecodeFrameInto`

```go
func (d *decoder) DecodeFrameInto(f *Frame) error
```

Parses the next frame into `f`, reusing its storage. A caller that passes the
same `f` in a loop makes no per-frame allocation: the raw frame is copied
straight into `f`'s own raw storage, so the returned `Data` window points into
`f` and stays valid after the next call.

The error returns of `Decode` apply unchanged. On error `f` is left untouched
(the strict `UnmarshalBinary` commits only after all checks pass), so a caller
may keep the previous contents and inspect `f` only on a nil error.

The parsing is as strict as `DecodeFrame`. `f` must not be copied while `Data`
is in use: a `Frame` keeps its raw storage in the value, and a copy keeps
pointing into the original.

```go
d := cantcp.NewDecoder(conn)
var f cantcp.Frame
for {
	if err := d.DecodeFrameInto(&f); err != nil {
		break
	}
	use(&f)
}
```

#### `Stats`, `ResetStats`

```go
func (d *decoder) Stats() Stats
func (d *decoder) ResetStats()
```

`Stats` returns a copy of the decoder stream counters, `ResetStats` zeroes
them. See `Stats` for the counter fields.

```go
d := cantcp.NewDecoder(conn)
if _, err := d.DecodeFrame(); err != nil {
	log.Println("decode:", err)
	return
}
fmt.Println("skipped:", d.Stats().Skipped)
d.ResetStats()
fmt.Println("after reset:", d.Stats().Skipped)
```

## `NewEncoder` — the encoder

```go
func NewEncoder(w io.Writer, opts ...option) *encoder
```

Returns an encoder writing packets to any `io.Writer`. The returned type is not
exported, like the parser of `New`: an encoder is used through the returned
value and its exported methods only. Options configure it exactly like `New`.
The encoder is not safe for concurrent use: use one encoder per stream.

```go
e := cantcp.NewEncoder(conn)
```

### Encoder methods

#### `Encode`

```go
func (e *encoder) Encode(frame []byte) error
```

Writes a packet carrying the raw frame: `16` bytes build a `TypeClassic`
packet, `72` bytes a `TypeFd` packet. Validation errors of `Encode`
(`ErrFrameLen`, `ErrBadDLC`, `ErrBadLen`) leave the stream untouched and the
buffer reusable.

A short write returns `io.ErrShortWrite`; a write error is returned unchanged
and leaves the stream in an unknown state.

```go
e := cantcp.NewEncoder(conn)
raw := make([]byte, 16) // struct can_frame
if err := e.Encode(raw); err != nil {
	log.Println("encode:", err)
}
```

#### `EncodeFrame`

```go
func (e *encoder) EncodeFrame(f *Frame) error
```

Marshals `f` with `Frame.MarshalBinary` and writes the packet. Marshal errors
(`ErrBadType`, `ErrBadID`, `ErrBadFlags`, `ErrBadDLC`, `ErrBadLen`) leave the
stream untouched.

```go
e := cantcp.NewEncoder(conn)
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
if err := e.EncodeFrame(&f); err != nil {
	log.Println("encode:", err)
}
```

## `Frame` — the parsed frame object

```go
type Frame struct {
	ID   uint32
	Type Type
	EFF  bool
	RTR  bool
	ERR  bool
	BRS  bool
	ESI  bool
	Data []byte
}
```

`Frame` is a parsed CAN or CAN FD frame, independent of the cantcp stream
framing: `MarshalBinary` and `UnmarshalBinary` work with the raw Linux
SocketCAN layouts (`struct can_frame`, 16 bytes, and `struct canfd_frame`,
72 bytes) only. Copying a `Frame` value copies the fields, but `Data` keeps
pointing into the raw storage of the original frame: use `GetRaw` for an
independent copy.

Public fields:

| Field | Type | Meaning |
|---|---|---|
| `ID` | `uint32` | Identifier: 11-bit standard, 29-bit with `EFF`; an error frame carries a 29-bit error class mask |
| `Type` | `Type` | `TypeClassic` or `TypeFd`; selects the frame layout |
| `EFF` | `bool` | Extended (29-bit) identifier |
| `RTR` | `bool` | Remote transmission request (classic CAN only) |
| `ERR` | `bool` | Error frame |
| `BRS` | `bool` | CAN FD bit rate switch |
| `ESI` | `bool` | CAN FD error state indicator |
| `Data` | `[]byte` | Payload; `len(Data)` is the frame length field (`can_dlc` / `len`) |

### `Frame.UnmarshalBinary`

```go
func (f *Frame) UnmarshalBinary(b []byte) error
```

Parses a raw frame: 16 bytes as `can_frame`, 72 bytes as `canfd_frame`, any
other length returns `ErrFrameLen`. Strict: `ErrBadDLC`, `ErrBadLen` (CAN FD
length not encodable in the 4-bit DLC field: only `0..8, 12, 16, 20, 24, 32,
48, 64` are valid), `ErrReserved` (non-zero padding/reserved bytes),
`ErrBadFlags` (`FlagRTR` in CAN FD, `FlagEFF` or `FlagRTR` with `FlagERR`,
unknown bits of the CAN FD flags byte; the kernel's `CANFD_FDF` 0x04 is
accepted and ignored), `ErrBadID` (an identifier that does not
fit the addressing mode; an error frame carries a 29-bit error class mask in
`ID` instead of an 11-bit identifier). On error the frame is left unchanged.

```go
raw := make([]byte, 16)     // struct can_frame
raw[0], raw[1] = 0x23, 0x01 // can_id = 0x123 (little-endian)
raw[4] = 2                  // can_dlc
copy(raw[8:], []byte{0xDE, 0xAD})
var f cantcp.Frame
err := f.UnmarshalBinary(raw)
fmt.Println(f) // Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
```

### `Frame.MarshalBinary`

```go
func (f *Frame) MarshalBinary() ([]byte, error)
```

Builds the raw frame (16 bytes for `TypeClassic`, 72 bytes for `TypeFd`),
validates `Type`, ID, flags and `len(Data)`, stores the result in the internal
raw frame and returns a copy. Errors: `ErrBadType`, `ErrBadID` (11 bits for a
standard frame, 29 with `FlagEFF` or in an error frame), `ErrBadFlags`
(`FlagERR` with `FlagEFF` or `FlagRTR`, `FlagBRS`/`FlagESI` in classic CAN,
`FlagRTR` in CAN FD), `ErrBadDLC`, `ErrBadLen`.

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
raw, err := f.MarshalBinary()
fmt.Println(len(raw), err)     // 72 <nil>
fmt.Printf("%x\n", raw[:6])    // 230100000201
```

### `Frame.SetFlags`

```go
func (f *Frame) SetFlags(flags uint8) error
```

Replaces all flags. Build the set with bitwise OR from the `Flag` constants;
flags not valid for the current `Type` or unknown bits return `ErrBadFlags`,
`FlagERR` combined with `FlagEFF` or `FlagRTR` returns `ErrBadFlags` (an error
frame carries no addressing mode), an unset or unknown `Type` returns
`ErrBadType`.

```go
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x123}
err := f.SetFlags(cantcp.FlagEFF | cantcp.FlagRTR)
fmt.Println(f) // Frame{Type:CAN, ID:0x123, Flags:EFF|RTR, DLC:0, Data:}
```

### `Frame.GetRaw`

```go
func (f *Frame) GetRaw() []byte
```

Returns a copy of the stored raw frame: 16 bytes for `TypeClassic`, 72 bytes
for `TypeFd`; an unset or unknown `Type` returns `nil`.

```go
var f cantcp.Frame
if err := f.UnmarshalBinary(raw72); err != nil {
	log.Fatal(err)
}
fmt.Println(len(f.GetRaw())) // 72
```

### `Frame.String`

Implements `fmt.Stringer`:

```go
f := cantcp.Frame{Type: cantcp.TypeClassic, ID: 0x1ABCDE, EFF: true, Data: []byte{0xDE, 0xAD}}
fmt.Println(f) // Frame{Type:CAN, ID:0x1abcde, Flags:EFF, DLC:2, Data:dead}
```

## `Type`

```go
type Type uint8

const (
	TypeClassic Type = 0x01 // can_frame, 16 bytes
	TypeFd      Type = 0x02 // canfd_frame, 72 bytes
)
```

Selects the frame layout. The same values are used as the packet type byte of
the stream framing.

`Type.String()` returns `"CAN"`, `"CAN FD"` or `"unknown"`:

```go
fmt.Println(cantcp.TypeClassic, cantcp.TypeFd, cantcp.Type(0)) // CAN CAN FD unknown
```

## Flags

`Flag` constants describe a `Frame`; combine them with bitwise OR and pass the
result to `Frame.SetFlags`. `FlagEFF`, `FlagRTR` and `FlagERR` come from the
CAN identifier word; `FlagBRS` and `FlagESI` come from the CAN FD flags byte.
The values are internal to the object: `MarshalBinary` and `UnmarshalBinary`
map them to and from the raw SocketCAN bit positions.

| Constant | Raw bit | Meaning |
|---|---|---|
| `FlagEFF` | `can_id` 0x80000000 | extended (29-bit) identifier |
| `FlagRTR` | `can_id` 0x40000000 | remote transmission request (classic only) |
| `FlagERR` | `can_id` 0x20000000 | error frame |
| `FlagBRS` | `canfd flags` 0x01 | CAN FD bit rate switch |
| `FlagESI` | `canfd flags` 0x02 | CAN FD error state indicator |

```go
f := cantcp.Frame{Type: cantcp.TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
err := f.SetFlags(cantcp.FlagEFF | cantcp.FlagBRS)
fmt.Println(f, err) // Frame{Type:CAN FD, ID:0x123, Flags:EFF|BRS, Len:2, Data:0102} <nil>
```

## `Stats`

The counter object returned by `Stats()` of a parser or a decoder. A garbage
stream can drive the counters without bound: they saturate at `math.MaxInt`
instead of wrapping around.

```go
type Stats struct {
	Skipped   int // bytes discarded while resynchronizing
	Dropped   int // candidates rejected by CRC
	BadType   int // packets with an unknown type byte
	BadDLC    int // classic frames with can_dlc > 8
	BadLen    int // CAN FD frames with canfd len > 64
	Truncated int // bytes lost to a packet cut off at EOF
}
```

```go
_, _, _ = p.Split([]byte{0x01, 0x02}, false)
s := p.Stats()
fmt.Println(s.Skipped, s.Dropped) // 2 0
```

## `BadFramePolicy`

```go
type BadFramePolicy int

const (
	BadFrameSkip BadFramePolicy = iota // drop, count and keep parsing (default)
	BadFrameFail                       // stop and return ErrBadType/ErrBadDLC/ErrBadLen
)
```

Selects how `Split` reacts to a structurally invalid packet; set with
`WithBadFramePolicy`.

```go
p := cantcp.New(cantcp.WithBadFramePolicy(cantcp.BadFrameSkip))
_, _, _ = p.Split([]byte{0xC3, 0x3C, 0x7F}, false)
fmt.Println(p.Stats().BadType) // 1
```

## `Logger`, `LevelTrace`

```go
type Logger interface {
	Log(ctx context.Context, level slog.Level, msg string, args ...any)
}

const LevelTrace = slog.Level(-8)
```

`Logger` is a one-method interface; a `*slog.Logger` implements it directly,
`nil` disables logging. `LevelTrace` (`slog.Level(-8)`) enables per-packet
tracing, it is lower than `slog.LevelDebug` so tracing can be enabled on its
own.

> **Warning:** `LevelTrace` writes the full hex of the raw frame to the log,
> **including the payload** — machine telemetry and commands. This is sensitive
> data: tracing is meant for debugging on an isolated bench only. Do not enable
> `LevelTrace` in production, and treat traced logs as sensitive.

```go
p := cantcp.New(cantcp.WithLogger(slog.Default()), cantcp.WithLogLevel(cantcp.LevelTrace))
// every parsed frame, skipped byte and CRC mismatch is traced
```

## `ValidateRaw`

```go
func ValidateRaw(b []byte) (Type, error)
```

Validates a raw frame without the cantcp stream envelope and reports its type:
`16` bytes are a `can_frame` (`TypeClassic`), `72` bytes a `canfd_frame`
(`TypeFd`), any other length returns `ErrFrameLen`. The layout is selected by
the length only: a raw SocketCAN frame carries no separate CAN FD marker, the
frame type is defined by the socket it was read from.

The rules are the same as `Frame.UnmarshalBinary`, which shares the
implementation: `ErrBadID`, `ErrBadDLC`, `ErrBadLen`, `ErrBadFlags`,
`ErrReserved`; the first error is returned. For a length of `16` or `72` bytes
the type is reported even together with an error, on `ErrFrameLen` it is
unset. Data bytes beyond the length field are not validated. `ValidateRaw`
does not read the cantcp envelope: magic bytes, the packet type byte and the
CRC-8 belong to `Split` and `Decode` only.

```go
typ, err := cantcp.ValidateRaw(raw) // 16 or 72 bytes
if err != nil {
	log.Println("invalid frame:", err) // ErrBadID, ErrBadFlags, ...
}
fmt.Println(typ) // CAN, CAN FD or unknown on ErrFrameLen
```

## Errors

All errors are sentinels compared with `errors.Is`; the package does not use
`fmt`, error texts are static.

| Error | Meaning |
|---|---|
| `ErrFrameLen` | frame length is not 16 or 72 bytes |
| `ErrBadDLC` | classic `can_dlc > 8` |
| `ErrBadLen` | invalid CAN FD length (> 64 or not encodable in the 4-bit DLC field) |
| `ErrBadType` | unknown packet/frame type |
| `ErrBadFlags` | flags not valid for the frame type |
| `ErrBadID` | identifier does not fit the addressing mode |
| `ErrReserved` | non-zero reserved bytes |
| `ErrTruncated` | packet cut off at the end of the stream |

```go
raw := make([]byte, 16)
raw[4] = 9 // can_dlc > 8
_, err := cantcp.ValidateRaw(raw)
fmt.Println(errors.Is(err, cantcp.ErrBadDLC)) // true
```

## CAN FD data lengths

CAN FD data lengths are encoded in the 4-bit DLC field with a discrete scale,
so only the following lengths exist; any other length is rejected with
`ErrBadLen` (Linux `can_fd_len2dlc` rejects it too). `Split` and `Decode` stay
tolerant (`len ≤ 64`) for raw pass-through.

| DLC | 0..8 | 9 | 10 | 11 | 12 | 13 | 14 | 15 |
|---|---|---|---|---|---|---|---|---|
| bytes | 0..8 | 12 | 16 | 20 | 24 | 32 | 48 | 64 |

## Security

cantcp is a transport, not a security layer. It is designed to run **inside a
trusted perimeter** (a closed network segment). Authentication, channel
encryption, integrity protection and replay protection are not tasks of the
library: they are the responsibility of whoever applies it — TLS/mTLS, VPN,
network segmentation and application-level peer authentication. The CRC-8 is a
framing sanity check, not cryptographic integrity; a peer that can write to the
stream can forge, modify or replay any frame. `LevelTrace` logs the raw frame
including the payload and must not be enabled in production.

See [SECURITY.md](SECURITY.md) for the threat model, the CPU amplification
note and the deployment checklist. Russian translation: [SECURITY.ru.md](SECURITY.ru.md).

A complete TLS 1.3 server and client with mutual TLS — certificate loading,
`tls.Config` for both sides and the cantcp codec over the connection — lives
in [examples/tls](examples/tls/). The OpenSSL commands that generate the
certificates are in the cantcp documentation (`docs/TLS-KEYS.md` in the
[cantcp](https://github.com/burn-lab-dev/cantcp) repository).

## Notes

- `Split` is a self-synchronizing splitter: after a corrupt candidate the
  resynchronization may find a valid frame even inside its data area. This is
  intended.
- `Encode` is a raw pass-through layer: it does not sanitize padding or flags,
  while `Frame` is the strict model. Layers are intentionally separate.
- Copying a `Frame` value copies the fields, but `Data` keeps pointing into the
  raw storage of the original frame; use `GetRaw` for an independent copy.
- The CRC-8 is a framing sanity check, not cryptographic integrity: about one
  in 256 random candidates passes it.
- `LevelTrace` logs the raw frame including its data (payload): it is a
  debugging tool for an isolated bench, not a production logging level; see
  [SECURITY.md](SECURITY.md).
- An error frame carries no addressing mode: `FlagERR` combined with
  `FlagEFF` or `FlagRTR` is rejected with `ErrBadFlags`, and its 29-bit error
  class mask is accepted in `ID` instead of an 11-bit identifier. `FlagRTR` is
  classic-only. An identifier that does not fit the addressing mode is an
  error, not silently masked.
- `ValidateRaw` and `Frame.UnmarshalBinary` share one validation core, so both
  accept and reject exactly the same raw frames; `Split` stays tolerant and
  performs only the stream-level checks.
- `DecodeFrame` allocates one `Frame` per call (the value owns its raw
  storage). `DecodeFrameInto` reuses a caller-owned `Frame` and makes no
  allocation per frame: use it on the hot path.
- `NewDecoder` and `NewEncoder` return unexported types, like `New`: they are
  used through the constructor and the exported methods only.

## Dependencies

Standard library only. No external dependencies.

## License

MIT — see [LICENSE](LICENSE).
