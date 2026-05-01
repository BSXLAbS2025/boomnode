package mesh

import (
	"net"
	"time"
)

// Типы сообщений BoomMesh
type MeshType string

const (
	MeshPING      MeshType = "PING"
	MeshPONG      MeshType = "PONG"
	MeshANNOUNCE  MeshType = "ANNOUNCE"
	MeshFIND      MeshType = "FIND"
	MeshFOUND     MeshType = "FOUND"
	MeshHOLEPUNCH MeshType = "HOLEPUNCH"
)

// MeshPeer — информация о пире в оверлее
type MeshPeer struct {
	BMAddress string       `json:"bm_address"`
	PublicKey string       `json:"public_key"`
	Addrs     []net.UDPAddr `json:"addrs"`
	LastSeen  time.Time    `json:"last_seen"`
	IsRelay   bool         `json:"is_relay"`
}

// MeshMessage — сообщение BoomMesh
type MeshMessage struct {
	Type      MeshType  `json:"type"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Data      string    `json:"data,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
