package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/api"
	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/config"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
	"github.com/BSXLAbS2025/boomnode/internal/mesh"
	"github.com/BSXLAbS2025/boomnode/internal/storage"
)

const apiBase = "http://127.0.0.1:24555"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "run":
		runNode()
	case "peer":
		handlePeerCommand()
	case "msg":
		handleMsgCommand()
	case "echo":
		handleEchoCommand()
	case "export":
		handleExportCommand()
	case "import":
		handleImportCommand()
	case "status":
		handleStatusCommand()
	case "mesh":
		handleMeshCommand()
	case "dial":
    	handleDialCommand()
	case "radio":
    	handleRadioCommand()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("BoomNode - BoomNet node")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run                              Start node")
	fmt.Println("  status                           Show node status")
	fmt.Println("  peer add <bm-addr> <name>        Add peer by BM address")
	fmt.Println("  peer list                        List peers")
	fmt.Println("  msg <bm-addr> <subject> <body>   Send message to peer")
	fmt.Println("  msg self <subject> <body>        Send message to yourself")
	fmt.Println("  msg tcp:<host:port> <subj> <body> Send direct TCP message")
	fmt.Println("  echo list                        List echo areas")
	fmt.Println("  echo sub <name> [description]    Subscribe to echo")
	fmt.Println("  echo post <area> <subject> <body> Post to echo area")
	fmt.Println("  echo read <area>                 Read messages from echo")
	fmt.Println("  export <bm-addr> [file]          Export messages for sneakernet")
	fmt.Println("  import <file>                    Import messages from sneakernet")
	fmt.Println("  dial <device> <baud> <phone> <msg-to> <subj> <body>  Send via dial-up modem")
	fmt.Println("  radio <msg-to> <subj> <body>  Send message via radio (soundcard)")
	fmt.Println("  mesh list                        List known DHT peers")
}

// ============================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================

// callAPI отправляет запрос к API и выводит форматированный ответ
func callAPI(method, path string, body interface{}) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		fmt.Printf("Request error: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("API error: %v (is the node running?)\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	result, _ := io.ReadAll(resp.Body)

	// Пробуем отформатировать JSON
	prettyPrintJSON(string(result))
}

// prettyPrintJSON форматирует JSON для читаемого вывода
func prettyPrintJSON(raw string) {
	var data interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		// Не JSON — выводим как есть
		fmt.Print(raw)
		return
	}
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Print(raw)
		return
	}
	fmt.Println(string(formatted))
}
// downloadFile скачивает файл через API
func downloadFile(method, path string, body interface{}, filename string) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, _ := http.NewRequest(method, apiBase+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("API error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		result, _ := io.ReadAll(resp.Body)
		fmt.Println(string(result))
		os.Exit(1)
	}

	data, _ := io.ReadAll(resp.Body)
	os.WriteFile(filename, data, 0644)
	fmt.Printf("Downloaded to %s\n", filename)
}

// uploadFile загружает файл через API
func uploadFile(path, filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("File error: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	var buf bytes.Buffer
	io.Copy(&buf, file)

	req, _ := http.NewRequest("POST", apiBase+path, &buf)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("API error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	result, _ := io.ReadAll(resp.Body)
	fmt.Print(string(result))
}

// ============================================================
// RADIO HANDLER
// ============================================================

func handleRadioCommand() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: bn radio <msg-to> <subject> <body>")
		fmt.Println("Example: bn radio BM-RU-FRIEND \"Hello\" \"Radio test\"")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("radio-%d", time.Now().UnixNano()),
		From:      from,
		To:        os.Args[2],
		Subject:   os.Args[3],
		Body:      os.Args[4],
		Timestamp: time.Now(),
	}

	fmt.Println("=== RADIO TRANSMISSION ===")
	if err := boomex.SendMessageViaRadio(from, msg); err != nil {
		fmt.Printf("Radio error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Radio transmission completed.")
}

// ============================================================
// RUN
// ============================================================

func runNode() {
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.Open(cfg.Storage.DataDir, false)
	if err != nil {
		fmt.Printf("Storage error: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		keys, _ = crypto.GenerateKeys()
		crypto.SaveKeys(keys, cfg.Storage.DataDir)
	}

	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	pubKeyStr := fmt.Sprintf("%x", keys.PublicKey)

	fmt.Println("=== BoomNode v0.3.0-alpha ===")
	fmt.Println("BoomNet: Where Ideas Detonate")
	fmt.Println()
	fmt.Printf("Address:   %s\n", address)
	fmt.Printf("Node name: %s\n", cfg.Node.Name)
	fmt.Printf("Mode:      %s\n", cfg.Node.Mode)
	fmt.Println()

	dht := mesh.NewDHT()
	isRelay := cfg.Node.Mode == "relay"
	meshSrv, err := mesh.NewServer("0.0.0.0:24553", dht, address, pubKeyStr, isRelay)
	if err != nil {
		fmt.Printf("BoomMesh error: %v\n", err)
		os.Exit(1)
	}
	if err := meshSrv.Start(); err != nil {
		fmt.Printf("BoomMesh start error: %v\n", err)
		os.Exit(1)
	}
	defer meshSrv.Stop()

	handler := func(session *boomex.Session, msg *boomex.Message) {
		fmt.Printf("=== INCOMING MESSAGE ===\nFrom: %s\nSubject: %s\nBody: %s\n=========================\n", msg.From, msg.Subject, msg.Body)
		if msg.To != address && store.DB() != nil {
			boomex.StoreMessageForRelay(store.DB(), msg)
			fmt.Printf("Stored relay message: %s -> %s\n", msg.From, msg.To)
		}
	}

	boomexSrv := boomex.NewServer("0.0.0.0:24554", address, store.DB(), isRelay, cfg.Relay.Whitelist, handler)
	if err := boomexSrv.Start(); err != nil {
		fmt.Printf("BoomEx server error: %v\n", err)
		os.Exit(1)
	}

	apiSrv := api.NewServer("127.0.0.1:24555", store.DB(), keys, address, dht, meshSrv)
	go func() {
		if err := apiSrv.Start(); err != nil {
			fmt.Printf("API server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Println("Node is running. Press Ctrl+C to exit.")
	fmt.Println("Services: BoomEx :24554 | BoomMesh :24553 | API :24555")
	select {}
}

// ============================================================
// PEER
// ============================================================

func handlePeerCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn peer add <bm-addr> <name>")
		fmt.Println("       bn peer list")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "add":
		if len(os.Args) < 5 {
			fmt.Println("Usage: bn peer add <bm-addr> <name>")
			os.Exit(1)
		}
		callAPI("POST", "/api/peers", map[string]string{
			"address": os.Args[3],
			"name":    os.Args[4],
		})
	case "list":
		callAPI("GET", "/api/peers", nil)
	}
}

// ============================================================
// MSG
// ============================================================

func handleMsgCommand() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: bn msg <bm-addr> <subject> <body>")
		fmt.Println("       bn msg tcp:<host:port> <subject> <body>")
		fmt.Println("       bn msg self <subject> <body>")
		os.Exit(1)
	}

	// Загружаем конфиг
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

	// Пытаемся загрузить ключи
	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		fmt.Println("Keys not found. Please run './bn run' first to initialize the node.")
		os.Exit(1)
	}

	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)
	to := os.Args[2]

	var tcpAddr string

	if to == "self" {
		tcpAddr = "127.0.0.1:24554"
		to = from
	} else if len(to) > 4 && to[:4] == "tcp:" {
		tcpAddr = to[4:]
	} else {
		fmt.Printf("Peer %s not found in local DB.\n", to)
		fmt.Printf("Use direct TCP: bn msg tcp:<host:port> \"%s\" \"%s\"\n", os.Args[3], os.Args[4])
		fmt.Printf("Or add peer first:\n  bn peer add %s <name>\n", to)
		os.Exit(1)
	}

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Subject:   os.Args[3],
		Body:      os.Args[4],
		Timestamp: time.Now(),
	}

	fmt.Printf("Sending to %s...\n", tcpAddr)
	if err := boomex.SendMessageToPeer(tcpAddr, from, msg); err != nil {
		fmt.Printf("Send error: %v\n", err)
		os.Exit(1)
	}
}

// ============================================================
// DialUP-connect
// ============================================================

func handleDialCommand() {
	if len(os.Args) < 8 {
		fmt.Println("Usage: bn dial <device> <baud> <phone> <msg-to> <subj> <body>")
		fmt.Println("Example: bn dial /dev/ttyUSB0 9600 5551234 BM-RU-FRIEND \"Hello\" \"Dial-up test\"")
		os.Exit(1)
	}

	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	from := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	device := os.Args[2]
	baud := 9600
	fmt.Sscanf(os.Args[3], "%d", &baud)
	phone := os.Args[4]
	to := os.Args[5]
	subject := os.Args[6]
	body := os.Args[7]

	msg := boomex.Message{
		Type:      boomex.TypeMSG,
		ID:        fmt.Sprintf("dial-%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Subject:   subject,
		Body:      body,
		Timestamp: time.Now(),
	}

	fmt.Printf("=== DIAL-UP SESSION ===\n")
	fmt.Printf("Device: %s\nBaud: %d\nPhone: %s\nTo: %s\n", device, baud, phone, to)

	if err := boomex.SendMessageViaDialup(device, baud, phone, from, msg); err != nil {
		fmt.Printf("Dial-up error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Dial-up session completed.")
}

// ============================================================
// ECHO
// ============================================================

func handleEchoCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn echo list")
		fmt.Println("       bn echo sub <name> [description]")
		fmt.Println("       bn echo post <area> <subject> <body>")
		fmt.Println("       bn echo read <area>")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "list":
		callAPI("GET", "/api/echoes", nil)

	case "sub":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bn echo sub <name> [description]")
			os.Exit(1)
		}
		desc := ""
		if len(os.Args) > 4 {
			desc = os.Args[4]
		}
		callAPI("POST", "/api/echoes", map[string]string{
			"name":        os.Args[3],
			"description": desc,
		})

	case "post":
		if len(os.Args) < 6 {
			fmt.Println("Usage: bn echo post <area> <subject> <body>")
			os.Exit(1)
		}
		callAPI("POST", "/api/echo", map[string]string{
			"area":    os.Args[3],
			"subject": os.Args[4],
			"body":    os.Args[5],
		})

	case "read":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bn echo read <area>")
			os.Exit(1)
		}
		callAPI("GET", "/api/echo/read?area="+os.Args[3], nil)

	default:
		fmt.Printf("Unknown echo command: %s\n", os.Args[2])
	}
}

// ============================================================
// EXPORT
// ============================================================

func handleExportCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn export <bm-addr> [output-file]")
		os.Exit(1)
	}

	to := os.Args[2]
	filename := fmt.Sprintf("BoomMail_to_%s.bpack", to)
	if len(os.Args) > 3 {
		filename = os.Args[3]
	}

	downloadFile("POST", "/api/export", map[string]string{"to": to}, filename)
}

// ============================================================
// IMPORT
// ============================================================

func handleImportCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn import <file.bpack>")
		os.Exit(1)
	}

	uploadFile("/api/import", os.Args[2])
}

// ============================================================
// STATUS (исправлено!)
// ============================================================

func handleStatusCommand() {
	// Сначала пробуем получить статус через API (сервер запущен)
	resp, err := http.Get(apiBase + "/api/status")
	if err == nil {
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		prettyPrintJSON(string(result))
		return
	}

	// Сервер не запущен — читаем локальные данные
	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Println("Config not found. Run './bn run' first.")
		os.Exit(1)
	}

	keys, err := crypto.LoadKeys(cfg.Storage.DataDir)
	if err != nil {
		fmt.Println("Keys not found. Run './bn run' first.")
		os.Exit(1)
	}

	address := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	fmt.Println("Node is OFFLINE")
	fmt.Printf("Address:   %s\n", address)
	fmt.Printf("Node name: %s\n", cfg.Node.Name)
	fmt.Printf("Mode:      %s\n", cfg.Node.Mode)
	fmt.Printf("Data dir:  %s\n", cfg.Storage.DataDir)
}

// ============================================================
// MESH
// ============================================================

func handleMeshCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bn mesh list")
		os.Exit(1)
	}

	if os.Args[2] == "list" {
		callAPI("GET", "/api/mesh", nil)
	}
}
