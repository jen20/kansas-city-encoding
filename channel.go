package kcs

import (
	"math"
	"math/rand"
)

type Channel struct {
	Speed float64

	Wow     float64
	WowRate float64

	Bandwidth float64

	AGCDepth float64
	AGCRate  float64

	DropoutsPerSec float64
	DropoutMS      float64

	HumLevel float64
	HumHz    float64

	SNRdB float64

	ClipLevel float64

	Seed int64
}

func PerfectChannel() Channel {
	return Channel{Speed: 1, SNRdB: math.Inf(1), WowRate: 3, AGCRate: 2, HumHz: 60}
}

func TypicalCassette() Channel {
	return Channel{
		Speed: 1.02, Wow: 0.006, WowRate: 3.1,
		Bandwidth: 6000,
		AGCDepth:  0.15, AGCRate: 1.7,
		DropoutsPerSec: 0.5, DropoutMS: 4,
		HumLevel: 0.01, HumHz: 60,
		SNRdB:     28,
		ClipLevel: 0.95,
		Seed:      1975,
	}
}

func AwfulCassette() Channel {
	return Channel{
		Speed: 0.94, Wow: 0.02, WowRate: 4.3,
		Bandwidth: 3500,
		AGCDepth:  0.35, AGCRate: 2.9,
		DropoutsPerSec: 4, DropoutMS: 8,
		HumLevel: 0.04, HumHz: 60,
		SNRdB:     14,
		ClipLevel: 0.6,
		Seed:      1976,
	}
}

func (c Channel) Apply(in []float64, sampleRate int) []float64 {
	sr := float64(sampleRate)
	rng := rand.New(rand.NewSource(c.Seed))

	out := c.resample(in, sr)
	out = c.lowpass(out, sr)
	out = c.gainDrift(out, sr)
	out = c.dropouts(out, sr, rng)
	out = c.hum(out, sr)
	out = c.noise(out, rng)
	out = c.clip(out)
	return out
}

func (c Channel) resample(in []float64, sr float64) []float64 {
	speed := c.Speed
	if speed <= 0 {
		speed = 1
	}
	if speed == 1 && c.Wow == 0 {
		return append([]float64(nil), in...)
	}
	rate := c.WowRate
	if rate <= 0 {
		rate = 3
	}

	out := make([]float64, 0, int(float64(len(in))/speed)+8)
	pos := 0.0
	for pos < float64(len(in)-1) {
		i := int(pos)
		f := pos - float64(i)
		out = append(out, in[i]*(1-f)+in[i+1]*f)

		step := speed
		if c.Wow != 0 {
			t := float64(len(out)) / sr
			step *= 1 + c.Wow*math.Sin(2*math.Pi*rate*t)
		}
		pos += step
	}
	return out
}

func (c Channel) lowpass(in []float64, sr float64) []float64 {
	if c.Bandwidth <= 0 || c.Bandwidth >= sr/2 {
		return in
	}
	a := 1 - math.Exp(-2*math.Pi*c.Bandwidth/sr)
	var y float64
	for i, x := range in {
		y += a * (x - y)
		in[i] = y
	}
	return in
}

func (c Channel) gainDrift(in []float64, sr float64) []float64 {
	if c.AGCDepth <= 0 {
		return in
	}
	rate := c.AGCRate
	if rate <= 0 {
		rate = 2
	}
	for i := range in {
		t := float64(i) / sr
		in[i] *= 1 + c.AGCDepth*math.Sin(2*math.Pi*rate*t)
	}
	return in
}

func (c Channel) dropouts(in []float64, sr float64, rng *rand.Rand) []float64 {
	if c.DropoutsPerSec <= 0 || c.DropoutMS <= 0 {
		return in
	}
	span := int(c.DropoutMS * sr / 1000)
	if span < 1 {
		span = 1
	}
	n := int(c.DropoutsPerSec * float64(len(in)) / sr)
	for k := 0; k < n; k++ {
		start := rng.Intn(len(in))
		for i := start; i < start+span && i < len(in); i++ {
			in[i] *= 0.02
		}
	}
	return in
}

func (c Channel) hum(in []float64, sr float64) []float64 {
	if c.HumLevel <= 0 {
		return in
	}
	f := c.HumHz
	if f <= 0 {
		f = 60
	}
	for i := range in {
		t := float64(i) / sr
		in[i] += c.HumLevel * math.Sin(2*math.Pi*f*t)
	}
	return in
}

func (c Channel) noise(in []float64, rng *rand.Rand) []float64 {
	if math.IsInf(c.SNRdB, 1) || math.IsNaN(c.SNRdB) {
		return in
	}
	var sum float64
	for _, x := range in {
		sum += x * x
	}
	if len(in) == 0 || sum == 0 {
		return in
	}
	rms := math.Sqrt(sum / float64(len(in)))
	sigma := rms / math.Pow(10, c.SNRdB/20)
	for i := range in {
		in[i] += rng.NormFloat64() * sigma
	}
	return in
}

func (c Channel) clip(in []float64) []float64 {
	if c.ClipLevel <= 0 {
		return in
	}
	for i, x := range in {
		if x > c.ClipLevel {
			in[i] = c.ClipLevel
		} else if x < -c.ClipLevel {
			in[i] = -c.ClipLevel
		}
	}
	return in
}
