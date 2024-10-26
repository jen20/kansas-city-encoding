package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/kcs-talk/kcs"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "encode":
		err = cmdEncode(os.Args[2:])
	case "decode":
		err = cmdDecode(os.Args[2:])
	case "analyze":
		err = cmdAnalyze(os.Args[2:])
	case "stress":
		err = cmdStress(os.Args[2:])
	case "sweep":
		err = cmdSweep(os.Args[2:])
	case "profiles":
		cmdProfiles()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "kcs: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kcs:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kcs - the Kansas City Standard, encoded and decoded

  kcs encode    turn bytes into cassette audio
  kcs decode    turn cassette audio back into bytes
  kcs analyze   report tone statistics and a cycle histogram
  kcs stress    round-trip through a simulated cassette
  kcs sweep     map the decoder's tolerance to speed error and noise
  kcs profiles  list the built-in encodings

Run "kcs <command> -h" for the flags of each.
`)
}

func profileFlag(fs *flag.FlagSet) *string {
	return fs.String("profile", "kcs300", "encoding profile (see: kcs profiles)")
}

func resolve(name string) (kcs.Profile, error) { return kcs.ProfileByName(name) }

func readInput(path, text string) ([]byte, error) {
	switch {
	case text != "":
		return []byte(text), nil
	case path == "" || path == "-":
		return io.ReadAll(os.Stdin)
	default:
		return os.ReadFile(path)
	}
}

func cmdEncode(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	profile := profileFlag(fs)
	in := fs.String("in", "", "input file (default: stdin)")
	text := fs.String("text", "", "literal text to encode instead of a file")
	out := fs.String("out", "out.wav", "output WAV file")
	rate := fs.Int("rate", 44100, "sample rate in Hz")
	wave := fs.String("wave", "sine", "tone shape: sine or square")
	amp := fs.Float64("amp", 0.7, "peak amplitude, 0..1")
	leader := fs.Float64("leader", 2.0, "seconds of leader tone (the standard suggests 5)")
	trailer := fs.Float64("trailer", 0.5, "seconds of trailer tone")
	fs.Parse(args)

	p, err := resolve(*profile)
	if err != nil {
		return err
	}
	data, err := readInput(*in, *text)
	if err != nil {
		return err
	}

	enc := kcs.NewEncoder(p)
	enc.SampleRate = *rate
	enc.Amplitude = *amp
	enc.Leader, enc.Trailer = *leader, *trailer
	if strings.EqualFold(*wave, "square") {
		enc.Wave = kcs.Square
	}

	samples, err := enc.Encode(data)
	if err != nil {
		return err
	}
	if err := kcs.WriteWAV(*out, samples, *rate); err != nil {
		return err
	}

	dur := float64(len(samples)) / float64(*rate)
	fmt.Printf("%s\n", p)
	fmt.Printf("encoded %d bytes -> %s\n", len(data), *out)
	fmt.Printf("  %.2f s at %d Hz, %s wave, %d frames of %d bits\n",
		dur, *rate, enc.Wave, len(data), p.FrameBits())
	fmt.Printf("  data occupies %.2f s; an 8 KB BASIC program would take %s\n",
		float64(len(data))/p.BytesPerSecond(), mmss(8192/p.BytesPerSecond()))
	return nil
}

func mmss(secs float64) string {
	m := int(secs) / 60
	s := secs - float64(m*60)
	if m == 0 {
		return fmt.Sprintf("%.0fs", s)
	}
	return fmt.Sprintf("%dm%02.0fs", m, s)
}

func cmdDecode(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	profile := profileFlag(fs)
	in := fs.String("in", "", "input WAV file")
	out := fs.String("out", "", "write decoded bytes here (default: stdout)")
	asHex := fs.Bool("hex", false, "print a hex dump instead of raw bytes")
	verbose := fs.Bool("v", false, "print per-frame detail and decoder statistics")
	fs.Parse(args)

	p, err := resolve(*profile)
	if err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("decode: -in is required")
	}
	samples, sr, err := kcs.ReadWAV(*in)
	if err != nil {
		return err
	}

	dec := kcs.NewDecoder(p, sr)
	res, err := dec.Decode(samples)
	if err != nil {
		return err
	}

	if *verbose {
		printStats(os.Stderr, p, res, sr)
		for _, f := range res.Frames {
			fmt.Fprintf(os.Stderr, "  %8.4fs  %02X  %s\n", f.Time, f.Byte, printable(f.Byte))
		}
	}

	switch {
	case *out != "":
		return os.WriteFile(*out, res.Data, 0o644)
	case *asHex:
		fmt.Print(hex.Dump(res.Data))
	default:
		os.Stdout.Write(res.Data)
		if len(res.Data) > 0 && res.Data[len(res.Data)-1] != '\n' {
			fmt.Println()
		}
	}
	return nil
}

func printable(b byte) string {
	if b >= 0x20 && b < 0x7F {
		return string(rune(b))
	}
	return "."
}

func printStats(w io.Writer, p kcs.Profile, res *kcs.Result, sr int) {
	sm, ssd, mm, msd, ns, nm := kcs.ToneStats(res.Cycles)
	fmt.Fprintf(w, "%s\n", p)
	fmt.Fprintf(w, "  audio      %.2f s at %d Hz\n", res.Duration(sr), sr)
	fmt.Fprintf(w, "  cycles     %d measured (%d space, %d mark)\n", len(res.Cycles), ns, nm)
	fmt.Fprintf(w, "  space tone %.1f Hz +/- %.1f (nominal %.0f)\n", sm, ssd, p.ZeroFreq)
	fmt.Fprintf(w, "  mark tone  %.1f Hz +/- %.1f (nominal %.0f)\n", mm, msd, p.OneFreq)
	fmt.Fprintf(w, "  speed      %.4fx nominal\n", res.SpeedFactor)
	fmt.Fprintf(w, "  frames     %d decoded, %d resyncs, %d parity errors\n",
		len(res.Frames), res.Resyncs, res.ParityErr)
	for _, warn := range res.Warnings {
		fmt.Fprintf(w, "  warning    %s\n", warn)
	}
}

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	profile := profileFlag(fs)
	in := fs.String("in", "", "input WAV file")
	bins := fs.Int("bins", 24, "histogram bins")
	width := fs.Int("width", 44, "histogram width in characters")
	fs.Parse(args)

	p, err := resolve(*profile)
	if err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("analyze: -in is required")
	}
	samples, sr, err := kcs.ReadWAV(*in)
	if err != nil {
		return err
	}
	dec := kcs.NewDecoder(p, sr)
	res, err := dec.Decode(samples)
	if err != nil {
		return err
	}

	printStats(os.Stdout, p, res, sr)
	fmt.Printf("  decoded    %d bytes\n\n", len(res.Data))

	h := kcs.CycleHistogram(res.Cycles, *bins)
	peak := 0
	for _, c := range h.Bins {
		if c > peak {
			peak = c
		}
	}
	thr := math.Sqrt(p.ZeroFreq*p.OneFreq) * res.SpeedFactor

	fmt.Println("  cycle frequency distribution (bar length is square-rooted,")
	fmt.Println("  so the small space-tone population stays visible)")
	fmt.Println()
	for i, c := range h.Bins {
		lo := h.Min + (h.Max-h.Min)*float64(i)/float64(*bins)
		hi := h.Min + (h.Max-h.Min)*float64(i+1)/float64(*bins)
		n := 0
		if peak > 0 && c > 0 {
			n = 1 + int(math.Sqrt(float64(c)/float64(peak))*float64(*width-1))
		}
		mark := " "
		if thr >= lo && thr < hi {
			mark = "<"
		}
		fmt.Printf("  %7.0f Hz |%s%-*s %d\n", lo, mark, *width, strings.Repeat("#", n), c)
	}
	fmt.Println()
	fmt.Printf("  Two populations, a wide empty valley between them. That gap is the\n")
	fmt.Printf("  noise margin, and '<' marks the decision threshold at %.0f Hz - the\n", thr)
	fmt.Printf("  geometric mean of the two tones, so a cycle has to be mismeasured by\n")
	fmt.Printf("  41%% before it is mistaken for the other tone.\n")
	return nil
}

func channelByName(name string) (kcs.Channel, error) {
	switch strings.ToLower(name) {
	case "perfect", "none":
		return kcs.PerfectChannel(), nil
	case "typical":
		return kcs.TypicalCassette(), nil
	case "awful":
		return kcs.AwfulCassette(), nil
	}
	return kcs.Channel{}, fmt.Errorf("unknown channel %q (have: perfect, typical, awful)", name)
}

func cmdStress(args []string) error {
	fs := flag.NewFlagSet("stress", flag.ExitOnError)
	profile := profileFlag(fs)
	in := fs.String("in", "", "input file (default: stdin)")
	text := fs.String("text", "GREETINGS FROM 1975, KANSAS CITY", "literal text to encode")
	channel := fs.String("channel", "typical", "channel model: perfect, typical, awful")
	speed := fs.Float64("speed", math.NaN(), "override playback speed (e.g. 1.1 for 10% fast)")
	snr := fs.Float64("snr", math.NaN(), "override signal-to-noise ratio in dB")
	wave := fs.String("wave", "sine", "tone shape: sine or square")
	rate := fs.Int("rate", 44100, "sample rate in Hz")
	save := fs.String("save", "", "write the damaged audio here for listening")
	fs.Parse(args)

	p, err := resolve(*profile)
	if err != nil {
		return err
	}
	ch, err := channelByName(*channel)
	if err != nil {
		return err
	}
	if !math.IsNaN(*speed) {
		ch.Speed = *speed
	}
	if !math.IsNaN(*snr) {
		ch.SNRdB = *snr
	}
	data, err := readInput(*in, *text)
	if err != nil {
		return err
	}

	enc := kcs.NewEncoder(p)
	enc.SampleRate = *rate
	enc.Leader, enc.Trailer = 0.5, 0.2
	if strings.EqualFold(*wave, "square") {
		enc.Wave = kcs.Square
	}
	clean, err := enc.Encode(data)
	if err != nil {
		return err
	}

	dirty := ch.Apply(clean, *rate)
	if *save != "" {
		if err := kcs.WriteWAV(*save, dirty, *rate); err != nil {
			return err
		}
	}

	dec := kcs.NewDecoder(p, *rate)
	res, err := dec.Decode(dirty)
	if err != nil {
		return err
	}

	fmt.Printf("channel '%s': speed %.3fx, wow %.1f%%, bandwidth %.0f Hz, SNR %.0f dB, %.1f dropouts/s\n",
		*channel, ch.Speed, ch.Wow*100, ch.Bandwidth, ch.SNRdB, ch.DropoutsPerSec)
	fmt.Println()
	printStats(os.Stdout, p, res, *rate)
	fmt.Println()

	fmt.Printf("  sent      %q\n", truncate(string(data), 60))
	fmt.Printf("  received  %q\n", truncate(string(res.Data), 60))
	if string(data) == string(res.Data) {
		fmt.Printf("\n  PERFECT RECOVERY: %d/%d bytes\n", len(res.Data), len(data))
		return nil
	}
	d := editDistance(data, res.Data)
	fmt.Printf("\n  %d edits from the original (%.0f%% intact)\n",
		d, 100*float64(max(0, len(data)-d))/float64(max(1, len(data))))
	fmt.Printf("  Note what is missing here: the 1975 standard has no checksum,\n")
	fmt.Printf("  no blocks and no way to retry. Acorn and the others had to add\n")
	fmt.Printf("  all three on top before tape was usable in the field.\n")
	return nil
}

func editDistance(a, b []byte) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func matching(a, b []byte) int {
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			n++
		}
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cmdSweep(args []string) error {
	fs := flag.NewFlagSet("sweep", flag.ExitOnError)
	profile := profileFlag(fs)
	text := fs.String("text", "KANSAS CITY STANDARD 1975", "payload for each trial")
	rate := fs.Int("rate", 44100, "sample rate in Hz")
	trials := fs.Int("trials", 5, "trials per cell, with different noise seeds")
	nofilter := fs.Bool("nofilter", false, "disable the decoder's input band-pass")
	fs.Parse(args)

	p, err := resolve(*profile)
	if err != nil {
		return err
	}
	data := []byte(*text)

	speeds := []float64{0.50, 0.70, 0.85, 0.95, 1.00, 1.05, 1.15, 1.40, 1.80}
	snrs := []float64{math.Inf(1), 20, 10, 6, 3, 0, -3, -6, -10}

	fmt.Printf("%s\n", p)
	if *nofilter {
		fmt.Printf("input band-pass: DISABLED\n")
	} else {
		fmt.Printf("input band-pass: %.0f Hz .. %.0f Hz\n", p.ZeroFreq/3, p.OneFreq*1.8)
	}
	fmt.Printf("\nBytes recovered, mean of %d trials (%d sent), by playback speed and noise:\n\n",
		*trials, len(data))
	fmt.Printf("  %-9s", "SNR\\spd")
	for _, s := range speeds {
		fmt.Printf(" %5.2fx", s)
	}
	fmt.Println()

	for _, snr := range snrs {
		label := "clean"
		if !math.IsInf(snr, 1) {
			label = fmt.Sprintf("%.0f dB", snr)
		}
		fmt.Printf("  %-9s", label)
		for _, sp := range speeds {
			total, perfect := 0, 0
			for t := 0; t < *trials; t++ {
				ch := kcs.PerfectChannel()
				ch.Speed = sp
				ch.SNRdB = snr
				ch.Seed = int64(1975 + t*7919)
				n := trial(p, data, ch, *rate, *nofilter)
				total += n
				if n == len(data) {
					perfect++
				}
			}
			switch {
			case perfect == *trials:
				fmt.Printf("    ok")
			case total == 0:
				fmt.Printf("     .")
			default:
				fmt.Printf("  %3.0f%%", 100*float64(total)/float64(*trials*len(data)))
			}
		}
		fmt.Println()
	}
	fmt.Printf("\n  'ok' = every byte of every trial recovered, '.' = nothing recovered.\n")
	fmt.Printf("  Across the speed axis the decoder never wavers: it consults no clock,\n")
	fmt.Printf("  it only counts cycles. Down the noise axis is where it finally dies.\n")
	if !*nofilter {
		fmt.Printf("  Try -nofilter to see what the input band-pass is worth.\n")
	}
	return nil
}

func trial(p kcs.Profile, data []byte, ch kcs.Channel, rate int, nofilter bool) int {
	enc := kcs.NewEncoder(p)
	enc.SampleRate = rate
	enc.Leader, enc.Trailer = 0.2, 0.1
	clean, err := enc.Encode(data)
	if err != nil {
		return 0
	}
	dec := kcs.NewDecoder(p, rate)
	dec.NoFilter = nofilter
	res, err := dec.Decode(ch.Apply(clean, rate))
	if err != nil {
		return 0
	}
	return matching(data, res.Data)
}

func cmdProfiles() {
	fmt.Println("Built-in profiles:")
	fmt.Println()
	for _, p := range kcs.Profiles {
		fmt.Printf("  %-12s %s\n", p.Name, p)
	}
	fmt.Println()
	fmt.Println("Every one satisfies the same invariant: a zero and a one take")
	fmt.Println("exactly the same time on tape. That is what makes them self-clocking.")
}
