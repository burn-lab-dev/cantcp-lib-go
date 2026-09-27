package cantcp

import (
	"errors"
	"slices"
	"testing"
)

func TestValidateRaw(t *testing.T) {
	classicPad := rawClassic(0x1, 0)
	classicPad[padOff] = 1
	classicRes0 := rawClassic(0x1, 0)
	classicRes0[res0Off] = 1
	classicLen8 := rawClassic(0x1, maxDLC, testPayload(maxDLC)...)
	classicLen8[res1Off] = 9
	fdRes0 := rawFd(0x1, 0, 0)
	fdRes0[res0Off] = 1
	fdRes1 := rawFd(0x1, 0, 0)
	fdRes1[res1Off] = 1
	classicTail := rawClassic(0x123, 1, 0xAA)
	classicTail[frameLen-1] = 0xFF
	fdTail := rawFd(0x123, 1, 0, 0xAA)
	fdTail[fdFrameLen-1] = 0xFF

	tests := []struct {
		name    string
		in      []byte
		wantTyp Type
		wantErr error
	}{
		// Length selects the layout: 16 bytes are classic CAN, 72 bytes CAN FD.
		{name: "nil", in: nil, wantErr: ErrFrameLen},
		{name: "empty", in: []byte{}, wantErr: ErrFrameLen},
		{name: "short classic", in: make([]byte, frameLen-1), wantErr: ErrFrameLen},
		{name: "long classic", in: make([]byte, frameLen+1), wantErr: ErrFrameLen},
		{name: "short can fd", in: make([]byte, fdFrameLen-1), wantErr: ErrFrameLen},
		{name: "long can fd", in: make([]byte, fdFrameLen+1), wantErr: ErrFrameLen},

		// Classic CAN, valid frames.
		{name: "classic empty", in: make([]byte, frameLen), wantTyp: TypeClassic},
		{name: "classic sff max id and dlc", in: rawClassic(canSFFMask, maxDLC, testPayload(maxDLC)...), wantTyp: TypeClassic},
		{name: "classic eff zero id", in: rawClassic(canEFFFlag, 0), wantTyp: TypeClassic},
		{name: "classic eff max id", in: rawClassic(canEFFFlag|canEFFMask, 0), wantTyp: TypeClassic},
		{name: "classic eff rtr", in: rawClassic(canEFFFlag|canRTRFlag|0x1ABCDE, 0), wantTyp: TypeClassic},
		{name: "classic eff rtr max id", in: rawClassic(canEFFFlag|canRTRFlag|canEFFMask, 0), wantTyp: TypeClassic},
		{name: "classic rtr", in: rawClassic(canRTRFlag|canSFFMask, 0), wantTyp: TypeClassic},
		{name: "classic err", in: rawClassic(canERRFlag|0x123, 1, 0xAA), wantTyp: TypeClassic},
		{name: "classic err zero class", in: rawClassic(canERRFlag, 0), wantTyp: TypeClassic},
		{name: "classic err 29 bit class", in: rawClassic(canERRFlag|canEFFMask, 0), wantTyp: TypeClassic},
		{name: "classic data tail not validated", in: classicTail, wantTyp: TypeClassic},

		// Classic CAN, errors.
		{name: "classic sff id too big", in: rawClassic(canSFFMask+1, 0), wantTyp: TypeClassic, wantErr: ErrBadID},
		{name: "classic rtr sff id too big", in: rawClassic(canRTRFlag|canSFFMask+1, 0), wantTyp: TypeClassic, wantErr: ErrBadID},
		{name: "classic err with eff", in: rawClassic(canERRFlag|canEFFFlag|0x123, 0), wantTyp: TypeClassic, wantErr: ErrBadFlags},
		{name: "classic err with rtr", in: rawClassic(canERRFlag|canRTRFlag|0x123, 0), wantTyp: TypeClassic, wantErr: ErrBadFlags},
		{name: "classic dlc 9", in: rawClassic(0x123, maxDLC+1), wantTyp: TypeClassic, wantErr: ErrBadDLC},
		{name: "classic dlc 255", in: rawClassic(0x123, 0xFF), wantTyp: TypeClassic, wantErr: ErrBadDLC},
		{name: "classic pad", in: classicPad, wantTyp: TypeClassic, wantErr: ErrReserved},
		{name: "classic res0", in: classicRes0, wantTyp: TypeClassic, wantErr: ErrReserved},
		{name: "classic len8 dlc", in: classicLen8, wantTyp: TypeClassic, wantErr: ErrReserved},

		// CAN FD, valid frames.
		{name: "can fd empty", in: make([]byte, fdFrameLen), wantTyp: TypeFd},
		{name: "can fd max len", in: rawFd(0x123, maxFDDataLen, 0, testPayload(maxFDDataLen)...), wantTyp: TypeFd},
		{name: "can fd discrete len", in: rawFd(0x123, 12, 0, testPayload(12)...), wantTyp: TypeFd},
		{name: "can fd eff", in: rawFd(canEFFFlag|canEFFMask, 0, 0), wantTyp: TypeFd},
		{name: "can fd brs", in: rawFd(0x123, 0, canFDBRS), wantTyp: TypeFd},
		{name: "can fd esi", in: rawFd(0x123, 0, canFDESI), wantTyp: TypeFd},
		{name: "can fd brs esi", in: rawFd(0x123, 0, canFDBRS|canFDESI), wantTyp: TypeFd},
		{name: "can fd err", in: rawFd(canERRFlag|0x123, 0, 0), wantTyp: TypeFd},
		{name: "can fd err 29 bit class brs", in: rawFd(canERRFlag|canEFFMask, 0, canFDBRS), wantTyp: TypeFd},
		{name: "can fd data tail not validated", in: fdTail, wantTyp: TypeFd},

		// CAN FD, errors.
		{name: "can fd len 9", in: rawFd(0x123, 9, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 10", in: rawFd(0x123, 10, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 11", in: rawFd(0x123, 11, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 13", in: rawFd(0x123, 13, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 63", in: rawFd(0x123, 63, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 65", in: rawFd(0x123, maxFDDataLen+1, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd len 255", in: rawFd(0x123, 0xFF, 0), wantTyp: TypeFd, wantErr: ErrBadLen},
		{name: "can fd fdf bit", in: rawFd(0x123, 0, 0x04), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd unknown flag bit", in: rawFd(0x123, 0, 0x80), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd all flag bits", in: rawFd(0x123, 0, 0xFF), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd rtr", in: rawFd(canRTRFlag|0x123, 0, 0), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd err with rtr", in: rawFd(canERRFlag|canRTRFlag|0x123, 0, 0), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd err with eff", in: rawFd(canERRFlag|canEFFFlag|0x123, 0, 0), wantTyp: TypeFd, wantErr: ErrBadFlags},
		{name: "can fd sff id too big", in: rawFd(canSFFMask+1, 0, 0), wantTyp: TypeFd, wantErr: ErrBadID},
		{name: "can fd res0", in: fdRes0, wantTyp: TypeFd, wantErr: ErrReserved},
		{name: "can fd res1", in: fdRes1, wantTyp: TypeFd, wantErr: ErrReserved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, err := ValidateRaw(tt.in)
			checkError(t, err, tt.wantErr)
			if typ != tt.wantTyp {
				t.Fatalf("ValidateRaw type = %v, want %v", typ, tt.wantTyp)
			}

			// ValidateRaw and Frame.UnmarshalBinary share checkRaw: both
			// accept and reject exactly the same frames.
			var f Frame
			frameErr := f.UnmarshalBinary(tt.in)
			if (err == nil) != (frameErr == nil) {
				t.Fatalf("ValidateRaw err = %v, UnmarshalBinary err = %v", err, frameErr)
			}
			if err != nil {
				if f.Type != 0 || f.ID != 0 || f.EFF || f.RTR || f.ERR || f.BRS || f.ESI || f.Data != nil {
					t.Fatalf("frame = %+v after an error, want zero", f)
				}
				return
			}
			if f.Type != typ {
				t.Fatalf("frame type = %v, ValidateRaw type = %v", f.Type, typ)
			}
		})
	}
}

func TestValidateRawClassicDLC(t *testing.T) {
	for dlc := range 256 {
		raw := rawClassic(0x123, byte(dlc))
		typ, err := ValidateRaw(raw)
		if typ != TypeClassic {
			t.Fatalf("dlc %d: type = %v, want %v", dlc, typ, TypeClassic)
		}
		if dlc <= maxDLC {
			if err != nil {
				t.Fatalf("dlc %d: err = %v, want nil", dlc, err)
			}
			continue
		}
		if !errors.Is(err, ErrBadDLC) {
			t.Fatalf("dlc %d: err = %v, want ErrBadDLC", dlc, err)
		}
	}
}

func TestValidateRawFDLen(t *testing.T) {
	validLens := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 12, 16, 20, 24, 32, 48, 64}
	for n := range 256 {
		raw := rawFd(0x123, byte(n), 0)
		typ, err := ValidateRaw(raw)
		if typ != TypeFd {
			t.Fatalf("len %d: type = %v, want %v", n, typ, TypeFd)
		}
		if slices.Contains(validLens, byte(n)) {
			if err != nil {
				t.Fatalf("len %d: err = %v, want nil", n, err)
			}
			continue
		}
		if !errors.Is(err, ErrBadLen) {
			t.Fatalf("len %d: err = %v, want ErrBadLen", n, err)
		}
	}
}
