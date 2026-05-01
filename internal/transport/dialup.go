package transport

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"github.com/tarm/serial"
)

// DialUpModem — соединение через dial-up модем
type DialUpModem struct {
	port   *serial.Port
	reader *bufio.Reader
}

// OpenModem открывает последовательный порт
func OpenModem(device string, baud int) (*DialUpModem, error) {
	c := &serial.Config{Name: device, Baud: baud, ReadTimeout: time.Second * 30}
	port, err := serial.OpenPort(c)
	if err != nil {
		return nil, fmt.Errorf("open modem: %w", err)
	}
	return &DialUpModem{port: port, reader: bufio.NewReader(port)}, nil
}

// Dial звонит на номер
func (m *DialUpModem) Dial(number string) error {
	m.port.Write([]byte("ATZ\r\n"))
	time.Sleep(time.Second)
	m.port.Write([]byte(fmt.Sprintf("ATDT%s\r\n", number)))
	time.Sleep(time.Second * 5)

	response, _ := m.reader.ReadString('\n')
	fmt.Printf("Modem: %s", response)
	return nil
}

// SendData отправляет данные
func (m *DialUpModem) SendData(data []byte) error {
	_, err := m.port.Write(data)
	return err
}

// ReceiveData принимает данные
func (m *DialUpModem) ReceiveData(buf []byte) (int, error) {
	return m.port.Read(buf)
}

// HangUp вешает трубку
func (m *DialUpModem) HangUp() error {
	m.port.Write([]byte("ATH0\r\n"))
	time.Sleep(time.Second)
	return m.port.Close()
}

// ReadLine читает строку
func (m *DialUpModem) ReadLine() (string, error) {
	return m.reader.ReadString('\n')
}

// Implement io.ReadWriter
func (m *DialUpModem) Read(p []byte) (n int, err error) {
	return m.port.Read(p)
}

func (m *DialUpModem) Write(p []byte) (n int, err error) {
	return m.port.Write(p)
}
