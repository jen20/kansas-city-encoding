package kcs

import (
	"fmt"
	"math"
)

type Cycle struct {
	Start  float64
	Period float64
	Freq   float64
	IsMark bool
}

type Frame struct {
	Byte       byte
	Time       float64
	CycleIndex int
	ParityOK   bool
}

type Result struct {
	Data   []byte
	Frames []Frame
	Cycles []Cycle

	SpeedFactor float64

	Resyncs   int
	ParityErr int
	Warnings  []string
}

func (r Result) Duration(sampleRate int) float64 {
	if len(r.Cycles) == 0 {
		return 0
	}
	last := r.Cycles[len(r.Cycles)-1]
	return (last.Start + last.Period) / float64(sampleRate)
}

type Decoder struct {
	Profile    Profile
	SampleRate int

	Hysteresis float64

	Squelch float64

	NoFilter bool

	maxWarnings int
}

func NewDecoder(p Profile, sampleRate int) *Decoder {
	return &Decoder{
		Profile:     p,
		SampleRate:  sampleRate,
		Hysteresis:  0.20,
		Squelch:     0.05,
		maxWarnings: 8,
	}
}

func (d *Decoder) Decode(samples []float64) (*Result, error) {
	if err := d.Profile.Validate(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return &Result{SpeedFactor: 1}, nil
	}

	sig := dcBlock(samples, float64(d.SampleRate))
	if !d.NoFilter {
		sig = d.bandpass(sig)
	}
	env := envelope(sig, float64(d.SampleRate))
	cross := d.crossings(sig, env)
	cycles := d.cycles(cross)
	speed := d.classify(cycles)

	res := &Result{Cycles: cycles, SpeedFactor: speed}
	d.extract(cycles, res)
	return res, nil
}

func dcBlock(in []float64, sr float64) []float64 {
	out := make([]float64, len(in))
	r := math.Exp(-2 * math.Pi * 20 / sr)
	var lastIn, lastOut float64
	for i, x := range in {
		y := x - lastIn + r*lastOut
		out[i] = y
		lastIn, lastOut = x, y
	}
	return out
}

func (d *Decoder) bandpass(in []float64) []float64 {
	sr := float64(d.SampleRate)
	out := biquad(in, sr, d.Profile.ZeroFreq/3, true)
	hi := d.Profile.OneFreq * 1.8
	if hi < sr/2*0.95 {
		out = biquad(out, sr, hi, false)
	}
	return out
}

func biquad(in []float64, sr, fc float64, highpass bool) []float64 {
	fwd := biquadOnce(in, sr, fc, highpass)
	reverse(fwd)
	back := biquadOnce(fwd, sr, fc, highpass)
	reverse(back)
	return back
}

func reverse(s []float64) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func biquadOnce(in []float64, sr, fc float64, highpass bool) []float64 {
	const q = 0.70710678
	w := 2 * math.Pi * fc / sr
	cw, sw := math.Cos(w), math.Sin(w)
	alpha := sw / (2 * q)

	var b0, b1, b2 float64
	if highpass {
		b0 = (1 + cw) / 2
		b1 = -(1 + cw)
		b2 = b0
	} else {
		b0 = (1 - cw) / 2
		b1 = 1 - cw
		b2 = b0
	}
	a0 := 1 + alpha
	a1 := -2 * cw
	a2 := 1 - alpha
	b0, b1, b2, a1, a2 = b0/a0, b1/a0, b2/a0, a1/a0, a2/a0

	out := make([]float64, len(in))
	var x1, x2, y1, y2 float64
	for i, x := range in {
		y := b0*x + b1*x1 + b2*x2 - a1*y1 - a2*y2
		out[i] = y
		x2, x1 = x1, x
		y2, y1 = y1, y
	}
	return out
}

func envelope(in []float64, sr float64) []float64 {
	out := make([]float64, len(in))
	attack := math.Exp(-1 / (0.001 * sr))
	release := math.Exp(-1 / (0.050 * sr))
	var e float64
	for i, x := range in {
		a := math.Abs(x)
		if a > e {
			e = attack*e + (1-attack)*a
		} else {
			e = release*e + (1-release)*a
		}
		out[i] = e
	}
	return out
}

func (d *Decoder) crossings(sig, env []float64) []float64 {
	peak := 0.0
	for _, e := range env {
		if e > peak {
			peak = e
		}
	}
	floor := peak * d.Squelch

	var out []float64
	armed := false
	for i := 1; i < len(sig); i++ {
		thr := env[i] * d.Hysteresis
		if env[i] < floor {
			armed = false
			continue
		}
		if sig[i] < -thr {
			armed = true
			continue
		}
		if armed && sig[i] > thr {

			j := i
			for j > 1 && sig[j-1] > 0 {
				j--
			}
			x0, x1 := sig[j-1], sig[j]
			frac := 0.0
			if x1 != x0 {
				frac = -x0 / (x1 - x0)
			}
			out = append(out, float64(j-1)+frac)
			armed = false
		}
	}
	return out
}

func (d *Decoder) cycles(cross []float64) []Cycle {
	sr := float64(d.SampleRate)

	minP := sr / (d.Profile.OneFreq * 3.0)
	maxP := sr / (d.Profile.ZeroFreq / 3.0)

	out := make([]Cycle, 0, len(cross))
	for i := 1; i < len(cross); i++ {
		p := cross[i] - cross[i-1]
		if p < minP || p > maxP {
			continue
		}
		out = append(out, Cycle{Start: cross[i-1], Period: p, Freq: sr / p})
	}
	return out
}

func (d *Decoder) classify(cycles []Cycle) float64 {
	if len(cycles) == 0 {
		return 1
	}
	speed := d.estimateSpeed(cycles)
	thr := math.Sqrt(d.Profile.ZeroFreq*d.Profile.OneFreq) * speed
	for i := range cycles {
		cycles[i].IsMark = cycles[i].Freq >= thr
	}
	return speed
}

func (d *Decoder) estimateSpeed(cycles []Cycle) float64 {

	const maxSamples = 20000
	step := 1 + len(cycles)/maxSamples

	logs := make([]float64, 0, len(cycles)/step+1)
	for i := 0; i < len(cycles); i += step {
		logs = append(logs, math.Log2(cycles[i].Freq))
	}
	lz := math.Log2(d.Profile.ZeroFreq)
	lo := math.Log2(d.Profile.OneFreq)

	const clamp = 0.25

	cost := func(ls float64) float64 {
		z, o := lz+ls, lo+ls
		total := 0.0
		for _, x := range logs {
			r := math.Min(math.Abs(x-z), math.Abs(x-o))
			if r > clamp {
				r = clamp
			}
			total += r * r
		}

		return total + float64(len(logs))*0.002*ls*ls
	}

	best, bestCost := 0.0, math.Inf(1)
	for ls := -1.3; ls <= 1.3; ls += 0.004 {
		if c := cost(ls); c < bestCost {
			best, bestCost = ls, c
		}
	}
	lo2, hi2 := best-0.004, best+0.004
	for i := 0; i < 40; i++ {
		m1 := lo2 + (hi2-lo2)/3
		m2 := hi2 - (hi2-lo2)/3
		if cost(m1) < cost(m2) {
			hi2 = m2
		} else {
			lo2 = m1
		}
	}
	return math.Exp2((lo2 + hi2) / 2)
}

func (d *Decoder) extract(cycles []Cycle, res *Result) {
	i := 0
	for i < len(cycles) {
		if cycles[i].IsMark {
			i++
			continue
		}
		b, next, parityOK, err := d.readFrame(cycles, i)
		if err != nil {
			if len(res.Warnings) < d.maxWarnings {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("%.3fs: %v", cycles[i].Start/float64(d.SampleRate), err))
			}
			res.Resyncs++
			i++
			continue
		}
		if !parityOK {
			res.ParityErr++
		}
		res.Frames = append(res.Frames, Frame{
			Byte:       b,
			Time:       cycles[i].Start / float64(d.SampleRate),
			CycleIndex: i,
			ParityOK:   parityOK,
		})
		res.Data = append(res.Data, b)
		i = next
	}
}

func (d *Decoder) readFrame(cycles []Cycle, start int) (b byte, next int, parityOK bool, err error) {
	cur := start
	p := d.Profile

	readBit := func() (int, error) {
		if cur >= len(cycles) {
			return 0, fmt.Errorf("ran off the end of the recording mid-frame")
		}
		mark := cycles[cur].IsMark
		n := p.ZeroCycles
		if mark {
			n = p.OneCycles
		}
		if cur+n > len(cycles) {
			return 0, fmt.Errorf("ran off the end of the recording mid-bit")
		}

		agree := 0
		for k := 0; k < n; k++ {
			if cycles[cur+k].IsMark == mark {
				agree++
			}
		}
		cur += n
		if agree*2 <= n {
			return 0, fmt.Errorf("bit cells disagree (%d/%d cycles)", agree, n)
		}
		if mark {
			return 1, nil
		}
		return 0, nil
	}

	bit, err := readBit()
	if err != nil {
		return 0, 0, false, err
	}
	if bit != 0 {
		return 0, 0, false, fmt.Errorf("start bit was a mark")
	}

	ones := 0
	for i := 0; i < p.DataBits; i++ {
		bit, err = readBit()
		if err != nil {
			return 0, 0, false, err
		}
		ones += bit
		b |= byte(bit) << uint(i)
	}

	parityOK = true
	if p.Parity != ParityNone {
		bit, err = readBit()
		if err != nil {
			return 0, 0, false, err
		}
		want := ones & 1
		if p.Parity == ParityOdd {
			want = 1 - want
		}
		parityOK = bit == want
	}

	for i := 0; i < p.StopBits; i++ {
		bit, err = readBit()
		if err != nil {
			return 0, 0, false, err
		}
		if bit != 1 {
			return 0, 0, false, fmt.Errorf("stop bit %d was a space (framing error)", i+1)
		}
	}
	return b, cur, parityOK, nil
}

type Histogram struct {
	Min, Max float64
	Bins     []int
}

func CycleHistogram(cycles []Cycle, nbins int) Histogram {
	h := Histogram{Bins: make([]int, nbins)}
	if len(cycles) == 0 || nbins < 1 {
		return h
	}
	h.Min, h.Max = math.Inf(1), math.Inf(-1)
	for _, c := range cycles {
		h.Min = math.Min(h.Min, c.Freq)
		h.Max = math.Max(h.Max, c.Freq)
	}
	if h.Max <= h.Min {
		h.Max = h.Min + 1
	}
	for _, c := range cycles {
		i := int((c.Freq - h.Min) / (h.Max - h.Min) * float64(nbins))
		if i >= nbins {
			i = nbins - 1
		}
		h.Bins[i]++
	}
	return h
}

func ToneStats(cycles []Cycle) (spaceMean, spaceSD, markMean, markSD float64, nSpace, nMark int) {
	var s1, s2, m1, m2 float64
	for _, c := range cycles {
		if c.IsMark {
			nMark++
			m1 += c.Freq
			m2 += c.Freq * c.Freq
		} else {
			nSpace++
			s1 += c.Freq
			s2 += c.Freq * c.Freq
		}
	}
	if nSpace > 0 {
		spaceMean = s1 / float64(nSpace)
		spaceSD = math.Sqrt(math.Max(0, s2/float64(nSpace)-spaceMean*spaceMean))
	}
	if nMark > 0 {
		markMean = m1 / float64(nMark)
		markSD = math.Sqrt(math.Max(0, m2/float64(nMark)-markMean*markMean))
	}
	return
}
