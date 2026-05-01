package transport

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"github.com/tarm/serial"
)

// DialUpConfig — настройки dial-up соединения
type DialUpConfig struct {
	Device     string // /dev/ttyUSB0 или COM1
	Baud       int    // 9600, 14400, 33600, 56000
	PhoneNumber string // номер телефона пира
	Timeout    time.Duration
}

// DialUpConn — реализует net.Conn для dial-up модема
type DialUpConn struct {
	port     *serial.Port
	reader   *bufio.Reader
	cfg      DialUpConfig
	isServer bool
}

// DialUpListener — слушает входящие звонки
type DialUpListener struct {
	port   *serial.Port
	reader *bufio.Reader
	cfg    DialUpConfig
}

// ============================================================
// КЛИЕНТ (звонящий)
// ============================================================

// DialUpDial звонит на указанный номер
func DialUpDial(cfg DialUpConfig) (*DialUpConn, error) {
	fmt.Printf("Opening modem on %s...\n", cfg.Device)

	c := &serial.Config{
		Name:        cfg.Device,
		Baud:        cfg.Baud,
		ReadTimeout: cfg.Timeout,
	}

	port, err := serial.OpenPort(c)
	if err != nil {
		return nil, fmt.Errorf("cannot open modem: %w", err)
	}

	conn := &DialUpConn{
		port:   port,
		reader: bufio.NewReader(port),
		cfg:    cfg,
	}

	// Инициализация модема
	fmt.Println("Initializing modem...")
	conn.sendAT("ATZ")     // Сброс
	time.Sleep(1 * time.Second)
	conn.sendAT("ATE0")    // Выключить эхо
	conn.sendAT("ATV1")    // Текстовый режим ответов
	conn.sendAT("AT&D2")   // Повесить трубку при DTR low

	// Звоним
	fmt.Printf("Dialing %s...\n", cfg.PhoneNumber)
	reply, err := conn.sendAT(fmt.Sprintf("ATDT%s", cfg.PhoneNumber))
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	// Ждём CONNECT
	if !conn.waitFor("CONNECT") {
		port.Close()
		return nil, fmt.Errorf("no carrier: %s", reply)
	}

	fmt.Println("Connected!")
	return conn, nil
}

// sendAT отправляет AT-команду и ждёт ответ
func (d *DialUpConn) sendAT(cmd string) (string, error) {
	_, err := d.port.Write([]byte(cmd + "\r\n"))
	if err != nil {
		return "", err
	}
	time.Sleep(500 * time.Millisecond)

	response := ""
	for {
		line, err := d.reader.ReadString('\n')
		if err != nil {
			break
		}
		response += line
		if line == "OK\r\n" || line == "ERROR\r\n" || line == "CONNECT\r\n" || line == "BUSY\r\n" || line == "NO CARRIER\r\n" {
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
// СЕРВЕР (принимающий звонки)
// ============================================================

// DialUpListen ждёт входящий звонок
func DialUpListen(cfg DialUpConfig) (*DialUpListener, error) {
	c := &serial.Config{
		Name:        cfg.Device,
		Baud:        cfg.Baud,
		ReadTimeout: cfg.Timeout,
	}

	port, err := serial.OpenPort(c)
	if err != nil {
		return nil, fmt.Errorf("cannot open modem: %w", err)
	}

	listener := &DialUpListener{
		port:   port,
		reader: bufio.NewReader(port),
		cfg:    cfg,
	}

	// Инициализация модема в режим ответа
	port.Write([]byte("ATZ\r\n"))
	time.Sleep(1 * time.Second)
	port.Write([]byte("ATS0=1\r\n")) // Автоответ после 1 гудка
	port.Write([]byte("ATE0\r\n"))
	port.Write([]byte("ATV1\r\n"))

	fmt.Printf("Modem listening on %s...\n", cfg.Device)
	return listener, nil
}

// Accept ждёт входящий звонок и возвращает соединение
func (l *DialUpListener) Accept() (*DialUpConn, error) {
	fmt.Println("Waiting for incoming call...")

	for {
		line, err := l.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read error: %w", err)
		}

		if line == "RING\r\n" {
			fmt.Println("RING detected! Answering...")
			time.Sleep(2 * time.Second) // Ждём ещё гудок

			// "Поднимаем трубку"
			l.port.Write([]byte("ATA\r\n"))

			// Ждём CONNECT от модема
			conn := &DialUpConn{
				port:   l.port,
				reader: l.reader,
				cfg:    l.cfg,
			}

			if conn.waitFor("CONNECT") {
				fmt.Println("Call accepted!")
				return conn, nil
			}
			fmt.Println("Failed to connect, waiting for next call...")
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
	return &dialAddr{addr: d.cfg.Device}
}

func (d *DialUpConn) RemoteAddr() net.Addr {
	return &dialAddr{addr: d.cfg.PhoneNumber}
}

func (d *DialUpConn) SetDeadline(t time.Time) error      { return nil }
func (d *DialUpConn) SetReadDeadline(t time.Time) error  { return nil }
func (d *DialUpConn) SetWriteDeadline(t time.Time) error { return nil }

type dialAddr struct {
	addr string
}

func (a *dialAddr) Network() string { return "dialup" }
func (a *dialAddr) String() string  { return a.addr }
