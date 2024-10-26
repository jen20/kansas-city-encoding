package kcs

import (
	"fmt"
	"math"
	"strings"
)

type Parity int

const (
	ParityNone Parity = iota
	ParityEven
	ParityOdd
)

func (p Parity) String() string {
	switch p {
	case ParityEven:
		return "E"
	case ParityOdd:
		return "O"
	default:
		return "N"
	}
}

type Profile struct {
	Name       string
	ZeroFreq   float64
	OneFreq    float64
	ZeroCycles int
	OneCycles  int
	DataBits   int
	StopBits   int
	Parity     Parity
}

func (p Profile) Baud() float64 { return p.ZeroFreq / float64(p.ZeroCycles) }

func (p Profile) BitPeriod() float64 { return 1 / p.Baud() }

func (p Profile) FrameBits() int {
	n := 1 + p.DataBits + p.StopBits
	if p.Parity != ParityNone {
		n++
	}
	return n
}

func (p Profile) BytesPerSecond() float64 {
	return p.Baud() / float64(p.FrameBits())
}

func (p Profile) Validate() error {
	if p.ZeroCycles < 1 || p.OneCycles < 1 {
		return fmt.Errorf("profile %s: cycle counts must be >= 1", p.Name)
	}
	if p.OneFreq <= p.ZeroFreq {
		return fmt.Errorf("profile %s: mark tone must be above space tone", p.Name)
	}
	tz := float64(p.ZeroCycles) / p.ZeroFreq
	to := float64(p.OneCycles) / p.OneFreq
	if math.Abs(tz-to) > 1e-12 {
		return fmt.Errorf("profile %s: bit periods differ (%.6g s vs %.6g s); "+
			"the encoding would not be self-clocking", p.Name, tz, to)
	}
	if p.DataBits < 5 || p.DataBits > 8 {
		return fmt.Errorf("profile %s: DataBits must be 5..8", p.Name)
	}
	if p.StopBits < 1 || p.StopBits > 2 {
		return fmt.Errorf("profile %s: StopBits must be 1 or 2", p.Name)
	}
	return nil
}

func (p Profile) String() string {
	return fmt.Sprintf("%s: %.0f baud, %.0f/%.0f Hz (%d/%d cycles), %d%s%d, %.1f B/s",
		p.Name, p.Baud(), p.ZeroFreq, p.OneFreq, p.ZeroCycles, p.OneCycles,
		p.DataBits, p.Parity, p.StopBits, p.BytesPerSecond())
}

var (
	KCS300 = Profile{
		Name: "kcs300", ZeroFreq: 1200, OneFreq: 2400,
		ZeroCycles: 4, OneCycles: 8,
		DataBits: 8, StopBits: 2, Parity: ParityNone,
	}

	KCS300P7 = Profile{
		Name: "kcs300-7e2", ZeroFreq: 1200, OneFreq: 2400,
		ZeroCycles: 4, OneCycles: 8,
		DataBits: 7, StopBits: 2, Parity: ParityEven,
	}

	CUTS1200 = Profile{
		Name: "cuts1200", ZeroFreq: 1200, OneFreq: 2400,
		ZeroCycles: 1, OneCycles: 2,
		DataBits: 8, StopBits: 2, Parity: ParityNone,
	}

	BBC1200 = Profile{
		Name: "bbc1200", ZeroFreq: 1200, OneFreq: 2400,
		ZeroCycles: 1, OneCycles: 2,
		DataBits: 8, StopBits: 1, Parity: ParityNone,
	}

	BBC300 = Profile{
		Name: "bbc300", ZeroFreq: 1200, OneFreq: 2400,
		ZeroCycles: 4, OneCycles: 8,
		DataBits: 8, StopBits: 1, Parity: ParityNone,
	}

	MSX2400 = Profile{
		Name: "msx2400", ZeroFreq: 2400, OneFreq: 4800,
		ZeroCycles: 1, OneCycles: 2,
		DataBits: 8, StopBits: 2, Parity: ParityNone,
	}
)

var Profiles = []Profile{KCS300, KCS300P7, CUTS1200, BBC300, BBC1200, MSX2400}

func ProfileByName(name string) (Profile, error) {
	for _, p := range Profiles {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	names := make([]string, len(Profiles))
	for i, p := range Profiles {
		names[i] = p.Name
	}
	return Profile{}, fmt.Errorf("unknown profile %q (have: %s)",
		name, strings.Join(names, ", "))
}
