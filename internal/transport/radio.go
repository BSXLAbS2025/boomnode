package transport

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net"
	"os/exec"
	"time"
)

// RadioConfig — конфигурация радио
type RadioConfig struct {
	MyCall     string
	Device     string
	Frequency  string
	Mode       string
	SampleRate int
	BitRate    int
}

// RadioConn — реализует net.Conn для радио
type RadioConn struct {
	cfg    RadioConfig
	reader *bufio.Reader
	writer io.Writer
	pipe   io.ReadCloser
	cmd    *exec.Cmd
}

// ============================================================
// AFSK-МОДЕМ
// ============================================================

const (
	markFreq  = 1200.0
	spaceFreq = 2200.0
)

func afskEncode(data []byte, sampleRate int, bitRate int) []int16 {
	samplesPerBit := sampleRate / bitRate
	output := make([]int16, 0)

	for _, b := range data {
		output = append(output, generateTone(spaceFreq, sampleRate, samplesPerBit)...)
		for i := 0; i < 8; i++ {
			bit := (b >> i) & 1
			if bit == 1 {
				output = append(output, generateTone(markFreq, sampleRate, samplesPerBit)...)
			} else {
				output = append(output, generateTone(spaceFreq, sampleRate, samplesPerBit)...)
			}
		}
		output = append(output, generateTone(markFreq, sampleRate, samplesPerBit)...)
	}

	return output
}

func generateTone(freq float64, sampleRate int, samples int) []int16 {
	tone := make([]int16, samples)
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := math.Sin(2 * math.Pi * freq * t)
		tone[i] = int16(sample * 32767 * 0.8)
	}
	return tone
}

func afskDecode(samples []int16, sampleRate int, bitRate int) []byte {
	samplesPerBit := sampleRate / bitRate
	output := make([]byte, 0)

	for i := 0; i < len(samples)-samplesPerBit*10; i += samplesPerBit / 2 {
		if detectFrequency(samples[i:i+samplesPerBit], sampleRate) > (markFreq+spaceFreq)/2 {
			continue
		}

		var b byte
		for bit := 0; bit < 8; bit++ {
			start := i + (bit+1)*samplesPerBit
			if start+samplesPerBit > len(samples) {
				break
			}
			freq := detectFrequency(samples[start:start+samplesPerBit], sampleRate)
			if freq > (markFreq+spaceFreq)/2 {
				b |= (1 << bit)
			}
		}
		output = append(output, b)
		i += samplesPerBit * 10
	}

	return output
}

func detectFrequency(samples []int16, sampleRate int) float64 {
	if len(samples) < 2 {
		return 0
	}
	crossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] < 0 && samples[i] >= 0) || (samples[i-1] >= 0 && samples[i] < 0) {
			crossings++
		}
	}
	return float64(crossings) * float64(sampleRate) / float64(2*len(samples))
}

// ============================================================
// РАДИО-ИНТЕРФЕЙС
// ============================================================

func RadioListen(cfg RadioConfig) (*RadioConn, error) {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 22050
	}
	if cfg.BitRate == 0 {
		cfg.BitRate = 1200
	}
	if cfg.Device == "" {
		cfg.Device = "soundcard"
	}
	if cfg.Frequency == "" {
		cfg.Frequency = "144.800"
	}

	fmt.Printf("Radio listening on %s MHz (%s mode)\n", cfg.Frequency, cfg.Mode)
	fmt.Printf("My call: %s\n", cfg.MyCall)

	var cmd *exec.Cmd
	var pipe io.ReadCloser

	switch cfg.Device {
	case "rtl":
		cmd = exec.Command("rtl_fm",
			"-f", cfg.Frequency+"e6",
			"-s", fmt.Sprintf("%d", cfg.SampleRate),
			"-")
		pipe, _ = cmd.StdoutPipe()
		cmd.Start()
		fmt.Println("Using RTL-SDR (receive only)")

	case "hackrf":
		cmd = exec.Command("hackrf_transfer",
			"-f", cfg.Frequency+"e6",
			"-s", fmt.Sprintf("%d", cfg.SampleRate),
			"-r", "-")
		pipe, _ = cmd.StdoutPipe()
		cmd.Start()
		fmt.Println("Using HackRF")

	default:
		cmd = exec.Command("arecord",
			"-f", "S16_LE",
			"-r", fmt.Sprintf("%d", cfg.SampleRate),
			"-c", "1",
			"-t", "raw",
			"-")
		pipe, _ = cmd.StdoutPipe()
		cmd.Start()
		fmt.Println("Using soundcard")
	}

	conn := &RadioConn{
		cfg:    cfg,
		reader: bufio.NewReader(pipe),
		pipe:   pipe,
		cmd:    cmd,
	}

	return conn, nil
}

func (r *RadioConn) Read(p []byte) (int, error) {
	samples := make([]int16, r.cfg.SampleRate)
	buf := make([]byte, len(samples)*2)
	n, err := r.reader.Read(buf)
	if err != nil {
		return 0, err
	}

	for i := 0; i < n/2; i++ {
		samples[i] = int16(buf[i*2]) | int16(buf[i*2+1])<<8
	}

	decoded := afskDecode(samples[:n/2], r.cfg.SampleRate, r.cfg.BitRate)
	copy(p, decoded)
	return len(decoded), nil
}

func (r *RadioConn) Write(p []byte) (int, error) {
	samples := afskEncode(p, r.cfg.SampleRate, r.cfg.BitRate)

	output := make([]byte, len(samples)*2)
	for i, s := range samples {
		output[i*2] = byte(s & 0xFF)
		output[i*2+1] = byte((s >> 8) & 0xFF)
	}

	switch r.cfg.Device {
	case "hackrf":
		cmd := exec.Command("hackrf_transfer",
			"-f", r.cfg.Frequency+"e6",
			"-s", fmt.Sprintf("%d", r.cfg.SampleRate),
			"-t", "-")
		stdin, _ := cmd.StdinPipe()
		cmd.Start()
		stdin.Write(output)
		stdin.Close()
		cmd.Wait()

	default:
		cmd := exec.Command("aplay",
			"-f", "S16_LE",
			"-r", fmt.Sprintf("%d", r.cfg.SampleRate),
			"-c", "1",
			"-t", "raw",
			"-")
		stdin, _ := cmd.StdinPipe()
		cmd.Start()
		stdin.Write(output)
		stdin.Close()
		cmd.Wait()
	}

	return len(p), nil
}

func (r *RadioConn) Close() error {
	fmt.Println("Stopping radio...")
	if r.cmd != nil {
		r.cmd.Process.Kill()
	}
	if r.pipe != nil {
		r.pipe.Close()
	}
	return nil
}

// ============================================================
// net.Conn ИНТЕРФЕЙС
// ============================================================

func (r *RadioConn) LocalAddr() net.Addr {
	return &radioAddr{fmt.Sprintf("radio:%s:%s", r.cfg.MyCall, r.cfg.Frequency)}
}

func (r *RadioConn) RemoteAddr() net.Addr {
	return &radioAddr{fmt.Sprintf("radio:remote:%s", r.cfg.Frequency)}
}

func (r *RadioConn) SetDeadline(t time.Time) error      { return nil }
func (r *RadioConn) SetReadDeadline(t time.Time) error  { return nil }
func (r *RadioConn) SetWriteDeadline(t time.Time) error { return nil }

type radioAddr struct {
	addr string
}

func (a *radioAddr) Network() string { return "radio" }
func (a *radioAddr) String() string  { return a.addr }
