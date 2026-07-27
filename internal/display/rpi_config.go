package display

// RPIConfig describes both the panel geometry and relevant native driver knobs.
type RPIConfig struct {
	Rows                   int
	Cols                   int
	ChainLength            int
	Parallel               int
	PWMBits                int
	PWMLSBNanoseconds      int
	PWMDitherBits          int
	Brightness             int
	ScanMode               int
	HardwareMapping        string
	ShowRefreshRate        bool
	InverseColors          bool
	DisableHardwarePulsing bool
	PixelMapperConfig      string
	LimitRefreshRateHz     int
	Multiplexing           int

	GPIOSlowdown int
}
