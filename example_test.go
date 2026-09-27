package cantcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
)

func ExampleNew() {
	p := New(
		WithMagic(0xC3, 0x3C),
		WithLogLevel(LevelTrace),
	)
	frame := make([]byte, frameLen) // classic can_frame
	pkt, _ := p.Encode(nil, frame)
	advance, _, _ := p.Split(pkt, true)
	fmt.Println(advance, p.Stats().BadDLC)
	// Output: 20 0
}

func ExampleNew_stats() {
	p := New()
	_, _, _ = p.Split([]byte{0x01, 0x02}, false)
	fmt.Println("skipped:", p.Stats().Skipped)
	p.ResetStats()
	fmt.Println("skipped after reset:", p.Stats().Skipped)
	// Output:
	// skipped: 2
	// skipped after reset: 0
}

func ExampleNew_split() {
	p := New()

	classic := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	classicRaw, _ := classic.MarshalBinary()
	classicPkt, _ := p.Encode(nil, classicRaw)

	fd := Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, Data: []byte{1, 2, 3}}
	fdRaw, _ := fd.MarshalBinary()
	fdPkt, _ := p.Encode(nil, fdRaw)

	sc := bufio.NewScanner(bytes.NewReader(append(classicPkt, fdPkt...)))
	sc.Split(p.Split)
	for sc.Scan() {
		var f Frame
		if err := f.UnmarshalBinary(sc.Bytes()); err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(f)
	}
	// Output:
	// Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
	// Frame{Type:CAN FD, ID:0x1abcde, Flags:EFF|BRS, Len:3, Data:010203}
}

func ExampleNew_encode() {
	p := New()
	frame := make([]byte, frameLen) // classic can_frame, 16 bytes
	pkt, err := p.Encode(nil, frame)
	fmt.Println(len(pkt), err)
	fmt.Printf("%x\n", pkt[:3])
	// Output:
	// 20 <nil>
	// c33c01
}

func ExampleNewDecoder() {
	p := New()
	classic := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	classicRaw, _ := classic.MarshalBinary()
	fd := Frame{Type: TypeFd, ID: 0x1ABCDE, EFF: true, BRS: true, Data: []byte{1, 2, 3}}
	fdRaw, _ := fd.MarshalBinary()
	classicPkt, _ := p.Encode(nil, classicRaw)
	fdPkt, _ := p.Encode(nil, fdRaw)

	d := NewDecoder(bytes.NewReader(slices.Concat(classicPkt, fdPkt)))
	for {
		f, err := d.DecodeFrame()
		if err != nil {
			break
		}
		fmt.Println(f)
	}
	// Output:
	// Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
	// Frame{Type:CAN FD, ID:0x1abcde, Flags:EFF|BRS, Len:3, Data:010203}
}

func Exampledecoder_Decode() {
	classic := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	raw, _ := classic.MarshalBinary()
	pkt, _ := New().Encode(nil, raw)

	d := NewDecoder(bytes.NewReader(pkt))
	frame, err := d.Decode()
	fmt.Printf("%x %v\n", frame, err)
	_, err = d.Decode()
	fmt.Println(err)
	// Output:
	// 2301000002000000dead000000000000 <nil>
	// EOF
}

func Exampledecoder_DecodeFrame() {
	classic := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	raw, _ := classic.MarshalBinary()
	pkt, _ := New().Encode(nil, raw)

	d := NewDecoder(bytes.NewReader(pkt))
	f, err := d.DecodeFrame()
	fmt.Println(f, err)
	// Output: Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead} <nil>
}

func Exampledecoder_DecodeFrameInto() {
	classic := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	raw, _ := classic.MarshalBinary()
	pkt, _ := New().Encode(nil, raw)

	// The same Frame is reused, so the loop makes no per-frame allocation.
	d := NewDecoder(bytes.NewReader(slices.Concat(pkt, pkt)))
	var f Frame
	for i := range 2 {
		if err := d.DecodeFrameInto(&f); err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(i, f)
	}
	fmt.Println(d.DecodeFrameInto(&f))
	// Output:
	// 0 Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
	// 1 Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
	// EOF
}

func Exampledecoder_Stats() {
	pkt, _ := New().Encode(nil, make([]byte, frameLen))
	// Two garbage bytes in front of the packet: the splitter skips them.
	d := NewDecoder(bytes.NewReader(append([]byte{0x01, 0x02}, pkt...)))

	if _, err := d.DecodeFrame(); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("skipped:", d.Stats().Skipped)
	// Output: skipped: 2
}

func Exampledecoder_ResetStats() {
	d := NewDecoder(bytes.NewReader([]byte{0x01, 0x02}))
	_, _ = d.Decode()
	d.ResetStats()
	fmt.Println("skipped after reset:", d.Stats().Skipped)
	// Output: skipped after reset: 0
}

func ExampleNewEncoder() {
	var buf bytes.Buffer
	e := NewEncoder(&buf)
	f := Frame{Type: TypeClassic, ID: 0x123, Data: []byte{0xDE, 0xAD}}
	if err := e.EncodeFrame(&f); err != nil {
		fmt.Println("error:", err)
		return
	}
	d := NewDecoder(&buf)
	got, err := d.DecodeFrame()
	fmt.Println(got, err)
	// Output: Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead} <nil>
}

func Exampleencoder_Encode() {
	classic := Frame{Type: TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
	raw, _ := classic.MarshalBinary()

	var buf bytes.Buffer
	e := NewEncoder(&buf)
	if err := e.Encode(raw); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(len(buf.Bytes()))
	// Output: 76
}

func Exampleencoder_EncodeFrame() {
	var buf bytes.Buffer
	e := NewEncoder(&buf)
	f := Frame{Type: TypeFd, ID: 0x123, BRS: true, Data: []byte{1, 2}}
	if err := e.EncodeFrame(&f); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(len(buf.Bytes()))
	// Output: 76
}

func ExampleWithMagic() {
	p := New(WithMagic(0x11, 0x22))
	frame := make([]byte, frameLen)
	pkt, _ := p.Encode(nil, frame)
	fmt.Printf("%x\n", pkt[:2])
	// Output: 1122
}

func ExampleWithCRCPoly() {
	p := New(WithCRCPoly(0x1D))
	frame := make([]byte, frameLen)
	pkt, _ := p.Encode(nil, frame)
	advance, token, _ := p.Split(pkt, true)
	fmt.Println(advance, bytes.Equal(token, frame))
	// Output: 20 true
}

func ExampleWithCRCCoverFrameOnly() {
	p := New(WithCRCCoverFrameOnly())
	frame := make([]byte, fdFrameLen) // canfd_frame, 72 bytes
	pkt, _ := p.Encode(nil, frame)
	advance, token, _ := p.Split(pkt, true)
	fmt.Println(advance, bytes.Equal(token, frame))
	// Output: 76 true
}

func ExampleWithBadFramePolicy() {
	p := New(WithBadFramePolicy(BadFrameFail))
	_, _, err := p.Split([]byte{0xC3, 0x3C, 0x7F}, false)
	fmt.Println(err)
	// Output: cantcp: unknown frame type
}

func ExampleWithLogger() {
	p := New(WithLogger(outLogger{}), WithLogLevel(LevelTrace))
	frame := make([]byte, frameLen)
	pkt, _ := p.Encode(nil, frame)
	_, _, _ = p.Split(pkt, true)
	// Output: frame parsed
}

func ExampleWithLogLevel() {
	p := New(WithLogger(outLogger{}), WithLogLevel(slog.LevelWarn))
	frame := make([]byte, frameLen)
	pkt, _ := p.Encode(nil, frame)
	_, _, _ = p.Split(pkt, true)
	fmt.Println("nothing was traced")
	// Output: nothing was traced
}

func ExampleType() {
	fmt.Println(TypeClassic, TypeFd, Type(0))
	// Output: CAN CAN FD unknown
}

func ExampleFrame_UnmarshalBinary() {
	raw := make([]byte, 16)     // struct can_frame
	raw[0], raw[1] = 0x23, 0x01 // can_id = 0x123 (little-endian)
	raw[4] = 2                  // can_dlc
	copy(raw[8:], []byte{0xDE, 0xAD})

	var f Frame
	if err := f.UnmarshalBinary(raw); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(f)
	// Output: Frame{Type:CAN, ID:0x123, Flags:none, DLC:2, Data:dead}
}

func ExampleValidateRaw() {
	classic := make([]byte, frameLen)   // struct can_frame
	classic[0], classic[1] = 0x23, 0x01 // can_id = 0x123 (little-endian)
	classic[dlcOff] = 2                 // can_dlc
	typ, err := ValidateRaw(classic)
	fmt.Println(typ, err)

	fd := make([]byte, fdFrameLen) // struct canfd_frame
	fd[0], fd[1] = 0x23, 0x01      // can_id = 0x123 (little-endian)
	fd[dlcOff] = 12                // canfd len
	fd[fdFlagsOff] = canFDBRS      // BRS
	typ, err = ValidateRaw(fd)
	fmt.Println(typ, err)

	bad := make([]byte, frameLen)
	bad[0], bad[1], bad[3] = 0x23, 0x01, 0xA0 // CAN_ERR_FLAG|CAN_EFF_FLAG|0x123
	typ, err = ValidateRaw(bad)
	fmt.Println(typ, err)
	// Output:
	// CAN <nil>
	// CAN FD <nil>
	// CAN cantcp: flags are not valid for the frame type
}

func ExampleFrame_MarshalBinary() {
	f := Frame{Type: TypeFd, ID: 0x123, BRS: true, Data: []byte{0x01, 0x02}}
	raw, err := f.MarshalBinary()
	fmt.Println(len(raw), err)
	fmt.Printf("%x\n", raw[:6])
	// Output:
	// 72 <nil>
	// 230100000201
}

func ExampleFrame_SetFlags() {
	f := Frame{Type: TypeClassic, ID: 0x123}
	if err := f.SetFlags(FlagEFF | FlagRTR); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(f)
	// Output: Frame{Type:CAN, ID:0x123, Flags:EFF|RTR, DLC:0, Data:}
}

func ExampleFrame_GetRaw() {
	raw := make([]byte, 72) // struct canfd_frame
	raw[4] = 64             // canfd len

	var f Frame
	if err := f.UnmarshalBinary(raw); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(len(f.GetRaw()))
	// Output: 72
}

func ExampleFrame_String() {
	f := Frame{Type: TypeClassic, ID: 0x1ABCDE, EFF: true, Data: []byte{0xDE, 0xAD}}
	fmt.Println(f)
	// Output: Frame{Type:CAN, ID:0x1abcde, Flags:EFF, DLC:2, Data:dead}
}

// outLogger is a minimal Logger that prints message texts; it is used by the
// logging examples.
type outLogger struct{}

func (outLogger) Log(_ context.Context, _ slog.Level, msg string, _ ...any) {
	fmt.Println(msg)
}
