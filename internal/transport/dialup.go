package transport

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"go.bug.st/serial"
)

// DialUpConfig — настройки dial-up соединения
type DialUpConfig struct {
	Device      string
	Baud        int
	PhoneNumber string
	Timeout     time.Duration
}

// DialUpConn — реализует net.Conn для dial-up модема
type DialUpConn struct {
	port   serial.Port
	reader *bufio.Reader
	cfg    DialUpConfig
}

// DialUpDial звонит на указанный номер
func DialUpDial(cfg DialUpConfig) (*DialUpConn, error) {
	fmt.Printf("Opening modem on %s at %d baud...\n", cfg.Device, cfg.Baud)

	mode := &serial.Mode{
		BaudRate: cfg.Baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return nil, fmt.Errorf("cannot open modem: %w", err)
	}

	// Установить таймаут
	port.SetReadTimeout(cfg.Timeout)

	conn := &DialUpConn{
		port:   port,
		reader: bufio.NewReader(port),
		cfg:    cfg,
	}

	// Инициализация модема
	fmt.Println("Initializing modem...")
	conn.sendAT("ATZ")
	time.Sleep(1 * time.Second)
	conn.sendAT("ATE0")
	conn.sendAT("ATV1")

	// Звоним
	fmt.Printf("Dialing %s...\n", cfg.PhoneNumber)
	_, err = conn.sendAT(fmt.Sprintf("ATDT%s", cfg.PhoneNumber))
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	if !conn.waitFor("CONNECT") {
		port.Close()
		return nil, fmt.Errorf("no carrier")
	}

	fmt.Println("Connected via dial-up!")
	return conn, nil
}

// sendAT отправляет AT-команду и ждёт ответ
func (d *DialUpConn) sendAT(cmd string) (string, error) {
	d.port.Write([]byte(cmd + "\r\n"))
	time.Sleep(500 * time.Millisecond)

	response := ""
	for {
		line, err := d.reader.ReadString('\n')
		if err != nil {
			break
		}
		response += line
		if len(line) > 0 && (line == "OK\r\n" || line == "ERROR\r\n" || line == "CONNECT\r\n" || line == "BUSY\r\n" || line == "NO CARRIER\r\n") {
			break
		}
	}
	return response, nil
}

// waitFor ждёт указанную строку в ответе модема
func (d *DialUpConn) waitFor(expected string) bool {
	timeout := time.After(d.cfg.Timeout)
	for {
		select {
		case <-timeout:
			return false
		default:
			line, err := d.reader.ReadString('\n')
			if err != nil {
				return false
			}
			if len(line) >= len(expected) && line[:len(expected)] == expected {
				return true
			}
		}
	}
}

// ============================================================
// net.Conn ИНТЕРФЕЙС
// ============================================================

func (d *DialUpConn) Read(p []byte) (n int, err error) {
	return d.port.Read(p)
}

func (d *DialUpConn) Write(p []byte) (n int, err error) {
	return d.port.Write(p)
}

func (d *DialUpConn) Close() error {
	fmt.Println("Hanging up...")
	d.port.Write([]byte("ATH0\r\n"))
	time.Sleep(1 * time.Second)
	return d.port.Close()
}

func (d *DialUpConn) LocalAddr() net.Addr {
	return &dialAddr{d.cfg.Device}
}

func (d *DialUpConn) RemoteAddr() net.Addr {
	return &dialAddr{d.cfg.PhoneNumber}
}

func (d *DialUpConn) SetDeadline(t time.Time) error {
	return d.port.SetReadTimeout(time.Until(t))
}

func (d *DialUpConn) SetReadDeadline(t time.Time) error {
	return d.port.SetReadTimeout(time.Until(t))
}

func (d *DialUpConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type dialAddr struct {
	addr string
}

func (a *dialAddr) Network() string { return "dialup" }
func (a *dialAddr) String() string  { return a.addr }
