package transport

import (
	"fmt"
	"os/exec"
)

// RadioAX25 — радио-транспорт через direwolf
type RadioAX25 struct {
	myCall  string
	direwolf *exec.Cmd
}

// StartRadio запускает direwolf (требует установленного direwolf)
func StartRadio(myCall string, freq string) (*RadioAX25, error) {
	cmd := exec.Command("direwolf", "-c", "/etc/direwolf.conf", "-t", "0")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start direwolf: %w", err)
	}
	fmt.Printf("Radio started on %s, call: %s\n", freq, myCall)
	return &RadioAX25{myCall: myCall, direwolf: cmd}, nil
}

// SendPacket отправляет пакет в эфир
func (r *RadioAX25) SendPacket(toCall string, data []byte) error {
	cmd := exec.Command("beacon", "-c", r.myCall, "-d", toCall, "-m", string(data))
	return cmd.Run()
}

// Stop останавливает радио
func (r *RadioAX25) Stop() error {
	if r.direwolf != nil {
		return r.direwolf.Process.Kill()
	}
	return nil
}
