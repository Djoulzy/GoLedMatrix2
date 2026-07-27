package display

import "testing"

func TestMappedGeometry(t *testing.T) {
	tests := []struct {
		name         string
		cols         int
		rows         int
		chain        int
		parallel     int
		mapperConfig string
		wantWidth    int
		wantHeight   int
	}{
		{
			name: "physical geometry without mapper",
			cols: 64, rows: 32, chain: 4, parallel: 2,
			wantWidth: 256, wantHeight: 64,
		},
		{
			name: "vertical arrangement",
			cols: 64, rows: 32, chain: 4, parallel: 2,
			mapperConfig: "V-mapper",
			wantWidth:    128, wantHeight: 128,
		},
		{
			name: "vertical serpentine arrangement",
			cols: 64, rows: 32, chain: 4, parallel: 2,
			mapperConfig: "V-mapper:Z",
			wantWidth:    128, wantHeight: 128,
		},
		{
			name: "mapper chain",
			cols: 64, rows: 32, chain: 4, parallel: 2,
			mapperConfig: " V-mapper ; Rotate:90 ; Mirror:H ",
			wantWidth:    128, wantHeight: 128,
		},
		{
			name: "rotate rectangular canvas",
			cols: 64, rows: 32, chain: 2, parallel: 1,
			mapperConfig: "Rotate:90",
			wantWidth:    32, wantHeight: 128,
		},
		{
			name: "stack parallel rows",
			cols: 64, rows: 32, chain: 2, parallel: 2,
			mapperConfig: "StackToRow:F",
			wantWidth:    256, wantHeight: 32,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height, err := MappedGeometry(
				test.cols, test.rows, test.chain, test.parallel, test.mapperConfig,
			)
			if err != nil {
				t.Fatal(err)
			}
			if width != test.wantWidth || height != test.wantHeight {
				t.Fatalf("geometry = %dx%d, want %dx%d", width, height, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestMappedGeometryRejectsInvalidMapper(t *testing.T) {
	tests := []string{
		"unknown",
		"V-mapper:invalid",
		"Rotate:45",
		"Mirror:diagonal",
		"StackToRow:X",
	}
	for _, mapper := range tests {
		t.Run(mapper, func(t *testing.T) {
			if _, _, err := MappedGeometry(64, 32, 4, 2, mapper); err == nil {
				t.Fatalf("MappedGeometry accepted mapper %q", mapper)
			}
		})
	}
}
