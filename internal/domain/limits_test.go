package domain

import (
	"strings"
	"testing"
)

func TestValidateNewDocument(t *testing.T) {
	cases := []struct {
		name               string
		width, height, res int
		wantErr            string // "" means valid
	}{
		{"typical HD", 1920, 1080, 72, ""},
		{"max side, valid surface", 30000, 6666, 9600, ""},
		{"width over max side", 30001, 100, 72, "边长"},
		{"height over max side", 100, 30001, 72, "边长"},
		{"huge sides caught as side, not overflow", 1_000_000, 1_000_000, 72, "边长"},
		{"surface over 200M", 30000, 7000, 72, "像素"},
		{"zero width", 0, 100, 72, "宽高"},
		{"negative height", 100, -5, 72, "宽高"},
		{"resolution zero", 100, 100, 0, "分辨率"},
		{"resolution over max", 100, 100, 9601, "分辨率"},
		{"resolution max accepted", 100, 100, 9600, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewDocument(tc.width, tc.height, tc.res)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateNewDocument(%d,%d,%d) = %v, want nil", tc.width, tc.height, tc.res, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateNewDocument(%d,%d,%d) = nil, want error containing %q", tc.width, tc.height, tc.res, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
