package kcs

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

func WriteWAV(path string, samples []float64, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const bits = 16
	dataLen := len(samples) * 2
	w := io.Writer(f)

	write := func(vals ...any) {
		for _, v := range vals {
			if err == nil {
				err = binary.Write(w, binary.LittleEndian, v)
			}
		}
	}

	w.Write([]byte("RIFF"))
	write(uint32(36 + dataLen))
	w.Write([]byte("WAVEfmt "))
	write(uint32(16), uint16(1), uint16(1), uint32(sampleRate),
		uint32(sampleRate*2), uint16(2), uint16(bits))
	w.Write([]byte("data"))
	write(uint32(dataLen))
	if err != nil {
		return err
	}

	buf := make([]byte, dataLen)
	for i, s := range samples {
		v := int32(math.Round(math.Max(-1, math.Min(1, s)) * 32767))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(v)))
	}
	_, err = f.Write(buf)
	return err
}

func ReadWAV(path string) ([]float64, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("%s: not a RIFF/WAVE file", path)
	}

	var (
		format     uint16
		channels   uint16
		sampleRate uint32
		bits       uint16
		data       []byte
		haveFmt    bool
	)

	for pos := 12; pos+8 <= len(raw); {
		id := string(raw[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(raw) {
			size = len(raw) - body
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("%s: short fmt chunk", path)
			}
			format = binary.LittleEndian.Uint16(raw[body:])
			channels = binary.LittleEndian.Uint16(raw[body+2:])
			sampleRate = binary.LittleEndian.Uint32(raw[body+4:])
			bits = binary.LittleEndian.Uint16(raw[body+14:])
			if format == 0xFFFE && size >= 26 {
				format = binary.LittleEndian.Uint16(raw[body+24:])
			}
			haveFmt = true
		case "data":
			data = raw[body : body+size]
		}
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}

	if !haveFmt || data == nil {
		return nil, 0, fmt.Errorf("%s: missing fmt or data chunk", path)
	}
	if channels == 0 {
		return nil, 0, fmt.Errorf("%s: zero channels", path)
	}

	bytesPer := int(bits) / 8
	if bytesPer == 0 {
		return nil, 0, fmt.Errorf("%s: unsupported bit depth %d", path, bits)
	}
	frameSize := bytesPer * int(channels)
	n := len(data) / frameSize
	out := make([]float64, n)

	for i := 0; i < n; i++ {
		var sum float64
		for c := 0; c < int(channels); c++ {
			off := i*frameSize + c*bytesPer
			v, err := decodeSample(data[off:off+bytesPer], format, bits)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", path, err)
			}
			sum += v
		}
		out[i] = sum / float64(channels)
	}
	return out, int(sampleRate), nil
}

func decodeSample(b []byte, format, bits uint16) (float64, error) {
	switch {
	case format == 3 && bits == 32:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), nil
	case format == 3 && bits == 64:
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
	case format == 1 && bits == 8:
		return (float64(b[0]) - 128) / 128, nil
	case format == 1 && bits == 16:
		return float64(int16(binary.LittleEndian.Uint16(b))) / 32768, nil
	case format == 1 && bits == 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xFFFFFF
		}
		return float64(v) / 8388608, nil
	case format == 1 && bits == 32:
		return float64(int32(binary.LittleEndian.Uint32(b))) / 2147483648, nil
	}
	return 0, fmt.Errorf("unsupported WAV format %d with %d bits", format, bits)
}
