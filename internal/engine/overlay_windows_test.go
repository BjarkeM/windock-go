package engine

import "testing"

func TestLightenMovesTowardsWhite(t *testing.T) {
	if got := lighten(0x000000, 1.0); got != 0xffffff {
		t.Errorf("lighten(black, 1.0) = %#06x, want 0xffffff", got)
	}
	if got := lighten(0x0078D7, 0); got != 0x0078D7 {
		t.Errorf("lighten(c, 0) = %#06x, want the colour unchanged", got)
	}
	got := lighten(0x0078D7, 0.45)
	if got == 0x0078D7 {
		t.Error("lighten should change the colour")
	}
	// Every channel must be at least as bright as it started.
	for _, sh := range []uint{16, 8, 0} {
		if (got>>sh)&0xff < (0x0078D7>>sh)&0xff {
			t.Errorf("channel at shift %d got darker: %#06x -> %#06x", sh, 0x0078D7, got)
		}
	}
}

func TestColorRefSwapsRedAndBlue(t *testing.T) {
	// 0xRRGGBB 0x0078D7 becomes COLORREF 0x00BBGGRR = 0xD77800.
	if got := colorRef(0x0078D7); got != 0xD77800 {
		t.Errorf("colorRef(0x0078D7) = %#06x, want 0xD77800", got)
	}
}
