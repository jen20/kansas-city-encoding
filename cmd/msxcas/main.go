package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kcs-talk/kcs"
)

const (
	longHeaderBits  = 16000 / 2
	shortHeaderBits = 4000 / 2
)

var (
	asciiMarker  = bytes.Repeat([]byte{0xEA}, 10)
	binaryMarker = bytes.Repeat([]byte{0xD0}, 10)
	basicMarker  = bytes.Repeat([]byte{0xD3}, 10)
)

const (
	blockSize = 256
	eofByte   = 0x1A
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "msxcas:", err)
		os.Exit(1)
	}
}

func run() error {
	fileType := flag.String("type", "ascii", "file type: ascii, basic or binary")
	name := flag.String("name", "KCSDEMO", "cassette file name (six characters, padded)")
	in := flag.String("in", "", "input file (default: stdin)")
	text := flag.String("text", "", "literal text to encode instead of a file")
	out := flag.String("out", "out.wav", "output WAV file")
	baud := flag.Int("baud", 1200, "cassette speed: 1200 or 2400")
	rate := flag.Int("rate", 44100, "sample rate in Hz")
	wave := flag.String("wave", "sine", "tone shape: sine or square")
	amp := flag.Float64("amp", 0.7, "peak amplitude, 0..1")
	flag.Parse()

	var profileName string
	switch *baud {
	case 1200:
		profileName = "cuts1200"
	case 2400:
		profileName = "msx2400"
	default:
		return fmt.Errorf("baud must be 1200 or 2400, not %d", *baud)
	}
	p, err := kcs.ProfileByName(profileName)
	if err != nil {
		return err
	}

	var marker []byte
	switch strings.ToLower(*fileType) {
	case "ascii":
		marker = asciiMarker
	case "basic":
		marker = basicMarker
	case "binary":
		marker = binaryMarker
	default:
		return fmt.Errorf("unknown file type %q; want ascii, basic or binary", *fileType)
	}

	data, err := readInput(*in, *text)
	if err != nil {
		return err
	}

	enc := kcs.NewEncoder(p)
	enc.SampleRate = *rate
	enc.Amplitude = *amp
	if strings.EqualFold(*wave, "square") {
		enc.Wave = kcs.Square
	}

	var samples []float64
	block := func(headerBits int, payload []byte) error {
		enc.Leader = float64(headerBits) / p.Baud()
		enc.Trailer = 0
		s, err := enc.Encode(payload)
		if err != nil {
			return err
		}
		samples = append(samples, s...)
		return nil
	}

	header := append(append([]byte(nil), marker...), padName(*name)...)
	if err := block(longHeaderBits, header); err != nil {
		return err
	}

	blocks := splitBlocks(data, strings.ToLower(*fileType) == "ascii")
	for _, b := range blocks {
		if err := block(shortHeaderBits, b); err != nil {
			return err
		}
	}

	enc.Leader, enc.Trailer = 0, 0.5
	tail, err := enc.Encode(nil)
	if err != nil {
		return err
	}
	samples = append(samples, tail...)

	if err := kcs.WriteWAV(*out, samples, *rate); err != nil {
		return err
	}

	dur := float64(len(samples)) / float64(*rate)
	fmt.Printf("%s\n", p)
	fmt.Printf("wrote %s: MSX %s file %q, %d baud\n", *out, strings.ToLower(*fileType), padName(*name), *baud)
	fmt.Printf("  %d data bytes in %d block(s) of %d, %.1f s total\n",
		len(data), len(blocks), blockSize, dur)
	fmt.Printf("  load it with: RUN\"CAS:\"   (ascii/basic)  or  BLOAD\"CAS:\",R  (binary)\n")
	return nil
}

func splitBlocks(data []byte, ascii bool) [][]byte {
	pad := byte(0)
	if ascii {
		data = append(append([]byte(nil), data...), eofByte)
		pad = eofByte
	}
	var blocks [][]byte
	for off := 0; off < len(data); off += blockSize {
		end := min(off+blockSize, len(data))
		b := make([]byte, blockSize)
		copy(b, data[off:end])
		for i := end - off; i < blockSize; i++ {
			b[i] = pad
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func padName(s string) string {
	s = strings.ToUpper(s)
	if len(s) > 6 {
		s = s[:6]
	}
	return s + strings.Repeat(" ", 6-len(s))
}

func readInput(path, text string) ([]byte, error) {
	switch {
	case text != "":

		t := strings.ReplaceAll(text, "\r\n", "\n")
		return []byte(strings.ReplaceAll(t, "\n", "\r\n") + "\r\n"), nil
	case path == "" || path == "-":
		return io.ReadAll(os.Stdin)
	default:
		return os.ReadFile(path)
	}
}
