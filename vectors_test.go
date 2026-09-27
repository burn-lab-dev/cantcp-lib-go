package cantcp

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// vectors is the cross-language test vector file: testdata/vectors.json holds
// streams, raw frames, fields, counters and errors of the protocol, so every
// implementation has to produce byte-for-byte identical results.
type vectors struct {
	Protocol string        `json:"protocol"`
	Version  string        `json:"version"`
	Config   vectorConfig  `json:"config"`
	CRC8     []vectorCRC8  `json:"crc8"`
	Frames   []vectorFrame `json:"frames"`
	Packets  []vectorPkt   `json:"packets"`
}

type vectorConfig struct {
	Magic    string `json:"magic"`
	CRCPoly  byte   `json:"crc_poly"`
	CRCCover string `json:"crc_cover"`
}

type vectorCRC8 struct {
	Name  string `json:"name"`
	Poly  byte   `json:"poly"`
	Input string `json:"input"`
	Sum   byte   `json:"sum"`
}

type vectorFrame struct {
	Name      string       `json:"name"`
	Raw       string       `json:"raw"`
	Fields    vectorFields `json:"fields"`
	Canonical string       `json:"canonical"`
	Error     string       `json:"error"`
}

type vectorFields struct {
	Type  string   `json:"type"`
	ID    uint32   `json:"id"`
	Flags []string `json:"flags"`
	Data  string   `json:"data"`
}

type vectorPkt struct {
	Name   string      `json:"name"`
	Stream string      `json:"stream"`
	Frames []string    `json:"frames"`
	Stats  vectorStats `json:"stats"`
	Error  string      `json:"error"`
	Policy string      `json:"bad_frame_policy"`
}

type vectorStats struct {
	Skipped   int `json:"skipped"`
	Dropped   int `json:"dropped"`
	BadType   int `json:"bad_type"`
	BadDLC    int `json:"bad_dlc"`
	BadLen    int `json:"bad_len"`
	Truncated int `json:"truncated"`
}

// stats converts the vector counters into the package type.
func (v vectorStats) stats() Stats {
	return Stats{
		Skipped:   v.Skipped,
		Dropped:   v.Dropped,
		BadType:   v.BadType,
		BadDLC:    v.BadDLC,
		BadLen:    v.BadLen,
		Truncated: v.Truncated,
	}
}

// vectorErrors maps a vector error name to the package sentinel. The empty
// name is the zero error.
var vectorErrors = map[string]error{
	"":             nil,
	"ErrFrameLen":  ErrFrameLen,
	"ErrBadDLC":    ErrBadDLC,
	"ErrBadLen":    ErrBadLen,
	"ErrBadType":   ErrBadType,
	"ErrBadFlags":  ErrBadFlags,
	"ErrBadID":     ErrBadID,
	"ErrReserved":  ErrReserved,
	"ErrTruncated": ErrTruncated,
}

// loadVectors reads and validates testdata/vectors.json.
func loadVectors(t *testing.T) vectors {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if v.Protocol != "cantcp" || v.Version != "v0" {
		t.Fatalf("protocol = %q version = %q, want cantcp v0", v.Protocol, v.Version)
	}
	if len(v.CRC8) == 0 || len(v.Frames) == 0 || len(v.Packets) == 0 {
		t.Fatal("vectors file has an empty section")
	}
	return v
}

// decodeHex decodes a hex string from the vector file.
func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// vectorError maps an error name from the vector file to its sentinel.
func vectorError(t *testing.T, name string) error {
	t.Helper()
	err, ok := vectorErrors[name]
	if !ok {
		t.Fatalf("unknown error name %q", name)
	}
	return err
}

// frameFields builds the expected Frame from the vector fields.
func frameFields(t *testing.T, vf vectorFields) Frame {
	t.Helper()
	f := Frame{ID: vf.ID, Data: decodeHex(t, vf.Data)}
	switch vf.Type {
	case "classic":
		f.Type = TypeClassic
	case "fd":
		f.Type = TypeFd
	default:
		t.Fatalf("unknown frame type %q", vf.Type)
	}
	for _, name := range vf.Flags {
		switch name {
		case "eff":
			f.EFF = true
		case "rtr":
			f.RTR = true
		case "err":
			f.ERR = true
		case "brs":
			f.BRS = true
		case "esi":
			f.ESI = true
		default:
			t.Fatalf("unknown frame flag %q", name)
		}
	}
	return f
}

func TestVectorConfig(t *testing.T) {
	v := loadVectors(t)
	p := New()
	if got := hex.EncodeToString(p.magic[:]); got != v.Config.Magic {
		t.Fatalf("magic = %s, want %s", got, v.Config.Magic)
	}
	if p.crc != newCRC8(v.Config.CRCPoly) {
		t.Fatalf("crc table = poly %#02x, want poly %#02x", v.Config.CRCPoly, 0x07)
	}
	if !p.coverHeader || v.Config.CRCCover != "magic+type+frame" {
		t.Fatalf("crc cover = %q (coverHeader = %v), want magic+type+frame", v.Config.CRCCover, p.coverHeader)
	}
}

func TestVectorCRC8(t *testing.T) {
	v := loadVectors(t)
	for _, tt := range v.CRC8 {
		t.Run(tt.Name, func(t *testing.T) {
			p := New(WithCRCPoly(tt.Poly))
			if got := p.crc.sum(decodeHex(t, tt.Input)); got != tt.Sum {
				t.Fatalf("crc8 = %#02x, want %#02x", got, tt.Sum)
			}
		})
	}
}

func TestVectorFrames(t *testing.T) {
	v := loadVectors(t)
	for _, tt := range v.Frames {
		t.Run(tt.Name, func(t *testing.T) {
			raw := decodeHex(t, tt.Raw)
			wantErr := vectorError(t, tt.Error)

			typ, validateErr := ValidateRaw(raw)
			checkError(t, validateErr, wantErr)

			var f Frame
			err := f.UnmarshalBinary(raw)
			checkError(t, err, wantErr)
			if tt.Error != "" {
				if f.Type != 0 || f.ID != 0 || f.EFF || f.RTR || f.ERR || f.BRS || f.ESI || f.Data != nil {
					t.Fatalf("frame = %+v after an error, want zero", f)
				}
				return
			}
			if typ != f.Type {
				t.Fatalf("ValidateRaw type = %v, frame type = %v", typ, f.Type)
			}

			checkFrame(t, f, frameFields(t, tt.Fields))

			canonical := tt.Canonical
			if canonical == "" {
				canonical = tt.Raw
			}
			got, err := f.MarshalBinary()
			checkError(t, err, nil)
			if !bytes.Equal(got, decodeHex(t, canonical)) {
				t.Fatalf("raw = %x, want canonical %s", got, canonical)
			}
			if raw := f.GetRaw(); !bytes.Equal(raw, got) {
				t.Fatalf("GetRaw = %x, want %x", raw, got)
			}
		})
	}
}

func TestVectorPackets(t *testing.T) {
	v := loadVectors(t)
	for _, tt := range v.Packets {
		t.Run(tt.Name, func(t *testing.T) {
			var opts []option
			switch tt.Policy {
			case "", "skip":
			case "fail":
				opts = append(opts, WithBadFramePolicy(BadFrameFail))
			default:
				t.Fatalf("unknown bad_frame_policy %q", tt.Policy)
			}

			stream := decodeHex(t, tt.Stream)
			d := NewDecoder(bytes.NewReader(stream), opts...)

			var frames [][]byte
			for {
				raw, err := d.Decode()
				if err != nil {
					wantErr := vectorError(t, tt.Error)
					if tt.Error == "" {
						wantErr = io.EOF
					}
					checkError(t, err, wantErr)
					break
				}
				frames = append(frames, raw)
			}
			if len(frames) != len(tt.Frames) {
				t.Fatalf("got %d frames, want %d", len(frames), len(tt.Frames))
			}
			for i := range frames {
				if want := decodeHex(t, tt.Frames[i]); !bytes.Equal(frames[i], want) {
					t.Fatalf("frame %d = %x, want %x", i, frames[i], want)
				}
			}
			if got := d.Stats(); got != tt.Stats.stats() {
				t.Fatalf("stats = %+v, want %+v", got, tt.Stats.stats())
			}

			// A stream that is exactly one valid packet without any dropped
			// bytes is the inverse of Encode.
			if tt.Error == "" && len(frames) == 1 && tt.Stats == (vectorStats{}) {
				var buf bytes.Buffer
				e := NewEncoder(&buf, opts...)
				if err := e.Encode(frames[0]); err != nil {
					t.Fatalf("Encode: %v", err)
				}
				if !bytes.Equal(buf.Bytes(), stream) {
					t.Fatalf("Encode = %x, want %x", buf.Bytes(), stream)
				}
			}
		})
	}
}
