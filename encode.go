package kcs

import (
	"fmt"
	"math"
)

type WaveShape int

const (
	Sine WaveShape = iota

	Square
)

func (w WaveShape) String() string {
	if w == Square {
		return "square"
	}
	return "sine"
}

type Encoder struct {
	Profile    Profile
	SampleRate int
	Amplitude  float64
	Wave       WaveShape
	Leader     float64
	Trailer    float64
}

func NewEncoder(p Profile) *Encoder {
	return &Encoder{
		Profile:    p,
		SampleRate: 44100,
		Amplitude:  0.7,
		Wave:       Sine,
		Leader:     2.0,
		Trailer:    0.5,
	}
}

func (e *Encoder) Encode(data []byte) ([]float64, error) {
	if err := e.Profile.Validate(); err != nil {
		return nil, err
	}
	if e.SampleRate < int(4*e.Profile.OneFreq) {
		return nil, fmt.Errorf("sample rate %d Hz is too low for a %.0f Hz mark tone; use at least %.0f Hz",
			e.SampleRate, e.Profile.OneFreq, 4*e.Profile.OneFreq)
	}

	est := int(float64(e.SampleRate) * (e.Leader + e.Trailer +
		float64(len(data))/e.Profile.BytesPerSecond()))
	g := &toneGen{
		sr:   float64(e.SampleRate),
		amp:  e.Amplitude,
		wave: e.Wave,
		out:  make([]float64, 0, est+e.SampleRate),
	}

	g.seconds(e.Profile.OneFreq, e.Leader)
	for _, b := range data {
		for _, bit := range e.Profile.frameBits(b) {
			e.emitBit(g, bit)
		}
	}
	g.seconds(e.Profile.OneFreq, e.Trailer)
	return g.out, nil
}

func (e *Encoder) emitBit(g *toneGen, bit int) {
	if bit == 1 {
		g.burst(e.Profile.OneFreq, e.Profile.OneCycles)
	} else {
		g.burst(e.Profile.ZeroFreq, e.Profile.ZeroCycles)
	}
}

func (p Profile) frameBits(b byte) []int {
	bits := make([]int, 0, p.FrameBits())
	bits = append(bits, 0)

	ones := 0
	for i := 0; i < p.DataBits; i++ {
		v := int(b>>uint(i)) & 1
		ones += v
		bits = append(bits, v)
	}

	switch p.Parity {
	case ParityNone:
	case ParityEven:
		bits = append(bits, ones&1)
	case ParityOdd:
		bits = append(bits, 1-(ones&1))
	}

	for i := 0; i < p.StopBits; i++ {
		bits = append(bits, 1)
	}
	return bits
}

type toneGen struct {
	sr   float64
	t    float64
	amp  float64
	wave WaveShape
	out  []float64
}

func (g *toneGen) burst(freq float64, cycles int) {
	end := g.t + float64(cycles)/freq*g.sr
	for i := len(g.out); float64(i) < end; i++ {
		phase := 2 * math.Pi * freq * (float64(i) - g.t) / g.sr
		g.out = append(g.out, g.shape(phase))
	}
	g.t = end
}

func (g *toneGen) seconds(freq, secs float64) {
	if secs <= 0 {
		return
	}
	g.burst(freq, int(math.Round(secs*freq)))
}

func (g *toneGen) shape(phase float64) float64 {
	s := math.Sin(phase)
	if g.wave == Square {
		if s >= 0 {
			return g.amp
		}
		return -g.amp
	}
	return g.amp * s
}
