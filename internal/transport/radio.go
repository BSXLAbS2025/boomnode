package transport

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os/exec"
)

// RadioConfig — конфигурация радио
type RadioConfig struct {
	MyCall     string // наш позывной/BM-адрес
	Device     string // "soundcard" (по умолчанию), "rtl" (RTL-SDR приём), "hackrf" (HackRF приём/передача)
	Frequency  string // частота в МГц, например "144.800"
	Mode       string // "afsk" (по умолчанию, совместимо с dial-up), "bpsk"
	SampleRate int    // частота дискретизации (по умолчанию 22050)
	BitRate    int    // битрейт (по умолчанию 1200)
}

// RadioConn — реализует io.ReadWriter для радио
type RadioConn struct {
	cfg    RadioConfig
	reader *bufio.Reader
	writer io.Writer
	pipe   io.ReadCloser
	cmd    *exec.Cmd
}

// ============================================================
// AFSK-МОДЕМ (ЧИСТЫЙ GO)
// ============================================================

const (
	markFreq  = 1200.0 // частота для "1"
	spaceFreq = 2200.0 // частота для "0"
)

// afskEncode кодирует байты в AFSK-аудио (16-bit PCM)
func afskEncode(data []byte, sampleRate int, bitRate int) []int16 {
	samplesPerBit := sampleRate / bitRate
	output := make([]int16, 0)

	for _, b := range data {
		// Стартовый бит (0)
		output = append(output, generateTone(spaceFreq, sampleRate, samplesPerBit)...)

		// 8 бит данных (LSB first)
		for i := 0; i < 8; i++ {
			bit := (b >> i) & 1
			if bit == 1 {
				output = append(output, generateTone(markFreq, sampleRate, samplesPerBit)...)
			} else {
				output = append(output, generateTone(spaceFreq, sampleRate, samplesPerBit)...)
			}
		}

		// Стоповый бит (1)
		output = append(output, generateTone(markFreq, sampleRate, samplesPerBit)...)
	}

	return output
}

// generateTone генерирует синусоиду заданной частоты
func generateTone(freq float64, sampleRate int, samples int) []int16 {
	tone := make([]int16, samples)
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := math.Sin(2 * math.Pi * freq * t)
		tone[i] = int16(sample * 32767 * 0.8) // 80% амплитуда
	}
	return tone
}

// afskDecode декодирует AFSK-аудио в байты (упрощённый вариант)
func afskDecode(samples []int16, sampleRate int, bitRate int) []byte {
	samplesPerBit := sampleRate / bitRate
	output := make([]byte, 0)

	// Ищем стартовый бит
	for i := 0; i < len(samples)-samplesPerBit*10; i += samplesPerBit / 2 {
		// Простое определение частоты: считаем переходы через ноль
		if detectFrequency(samples[i:i+samplesPerBit], sampleRate) > (markFreq+spaceFreq)/2 {
			// Нашли маркер — возможно, стоповый бит предыдущего байта
			continue
		}

		// Стартовый бит найден, декодируем 8 бит
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
		i += samplesPerBit * 10 // Переходим к следующему байту
	}

	return output
}

// detectFrequency определяет частоту сигнала (упрощённо)
func detectFrequency(samples []int16, sampleRate int) float64 {
	if len(samples) < 2 {
		return 0
	}

	// Считаем переходы через ноль
	crossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] < 0 && samples[i] >= 0) || (samples[i-1] >= 0 && samples[i] < 0) {
			crossings++
		}
	}

	// Частота = (crossings / 2) * (sampleRate / len(samples))
	return float64(crossings) * float64(sampleRate) / float64(2*len(samples))
}

// ============================================================
// РАДИО-ИНТЕРФЕЙС
// ============================================================

// RadioListen открывает радио на приём
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
		// RTL-SDR — только приём
		cmd = exec.Command("rtl_fm",
			"-f", cfg.Frequency+"e6",
			"-s", fmt.Sprintf("%d", cfg.SampleRate),
			"-")
		pipe, _ = cmd.StdoutPipe()
		cmd.Start()
		fmt.Println("Using RTL-SDR (receive only)")

	case "hackrf":
		// HackRF — приём и передача
		cmd = exec.Command("hackrf_transfer",
			"-f", cfg.Frequency+"e6",
			"-s", fmt.Sprintf("%d", cfg.SampleRate),
			"-r", "-")
		pipe, _ = cmd.StdoutPipe()
		cmd.Start()
		fmt.Println("Using HackRF")

	default:
		// Звуковая карта — используем arecord для приёма
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

// Read читает данные из радиоэфира и декодирует AFSK
func (r *RadioConn) Read(p []byte) (int, error) {
	// Читаем сырые аудио-сэмплы
	samples := make([]int16, r.cfg.SampleRate) // 1 секунда аудио
	buf := make([]byte, len(samples)*2)
	n, err := r.reader.Read(buf)
	if err != nil {
		return 0, err
	}

	// Конвертируем в int16
	for i := 0; i < n/2; i++ {
		samples[i] = int16(buf[i*2]) | int16(buf[i*2+1])<<8
	}

	// Декодируем AFSK
	decoded := afskDecode(samples[:n/2], r.cfg.SampleRate, r.cfg.BitRate)
	copy(p, decoded)
	return len(decoded), nil
}

// Write кодирует данные в AFSK и передаёт в эфир
func (r *RadioConn) Write(p []byte) (int, error) {
	samples := afskEncode(p, r.cfg.SampleRate, r.cfg.BitRate)

	// Конвертируем в байты
	output := make([]byte, len(samples)*2)
	for i, s := range samples {
		output[i*2] = byte(s & 0xFF)
		output[i*2+1] = byte((s >> 8) & 0xFF)
	}

	// Воспроизводим через динамик (aplay) или передаём через радио
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
		// Звуковая карта — воспроизводим
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

// Close останавливает радио
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
