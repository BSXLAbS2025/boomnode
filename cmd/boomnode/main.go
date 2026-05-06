package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BSXLAbS2025/boomnode/internal/api"
	"github.com/BSXLAbS2025/boomnode/internal/block"
	"github.com/BSXLAbS2025/boomnode/internal/boomex"
	"github.com/BSXLAbS2025/boomnode/internal/config"
	"github.com/BSXLAbS2025/boomnode/internal/crypto"
	"github.com/BSXLAbS2025/boomnode/internal/echo"
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
	case "ban":
		if len(os.Args) < 5 {
			fmt.Println("Usage: bn ban <echo-area> <bm-address> <reason>")
			os.Exit(1)
		}
		handleBanCommand()
	case "sign-block":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bn sign-block <block.json>")
			os.Exit(1)
		}
		signBlockFile(os.Args[2])
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
	fmt.Println("  ban <echo-area> <bm-addr> <reason>  Ban user from echo (moderator only)")
	fmt.Println("  sign-block <block.json>          Sign a block.json file with your key")
	fmt.Println("  mesh list                        List known DHT peers")
}

// ============================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================================

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
	prettyPrintJSON(string(result))
}

func prettyPrintJSON(raw string) {
	var data interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
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
// BAN / SIGN-BLOCK
// ============================================================

func handleBanCommand() {
	echoArea := os.Args[2]
	target := os.Args[3]
	reason := os.Args[4]

	cfg, _ := config.Load("boomnode.yaml")
	store, _ := storage.Open(cfg.Storage.DataDir, true)
	defer store.Close()

	// Проверяем существование эхи
	echoInfo, err := echo.GetEchoInfo(store.DB(), echoArea)
	if err != nil {
		fmt.Printf("Echo area '%s' not found\n", echoArea)
		os.Exit(1)
	}

	// Проверяем свои права
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)
	myAddress := crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo)

	if echoInfo.Moderator != myAddress {
		fmt.Printf("⛔ Access denied. Only moderator (%s) can ban users in '%s'\n", echoInfo.Moderator, echoArea)
		os.Exit(1)
	}

	// Добавляем локальный бан
	entry := block.BlockEntry{
		Address:  target,
		Reason:   reason,
		BannedBy: myAddress,
		Date:     time.Now().Format("2006-01-02"),
	}

	if err := echo.AddBan(store.DB(), echoArea, entry); err != nil {
		fmt.Printf("Error saving ban: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("❌ Banned %s from %s: %s\n", target, echoArea, reason)
	fmt.Printf("📝 To request global ban, send message to boombox.moderation\n")
}

func signBlockFile(path string) {
	cfg, _ := config.Load("boomnode.yaml")
	keys, _ := crypto.LoadKeys(cfg.Storage.DataDir)

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Cannot read file: %v\n", err)
		os.Exit(1)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	// Удаляем старые сигнатуры для чистоты данных
	delete(raw, "signatures")
	dataWithoutSigs, _ := json.Marshal(raw)

	// Подписываем
	sig := ed25519.Sign(keys.PrivateKey, dataWithoutSigs)
	sigHex := hex.EncodeToString(sig)

	// Добавляем подпись в массив
	sigs, _ := raw["signatures"].([]interface{})
	if sigs == nil {
		sigs = make([]interface{}, 0)
	}
	sigs = append(sigs, sigHex)
	raw["signatures"] = sigs

	signedData, _ := json.MarshalIndent(raw, "", "  ")
	os.WriteFile(path, signedData, 0644)

	fmt.Printf("✅ Signed %s successfully with key %s\n", path, crypto.AddressFromKey(keys.PublicKey, cfg.Node.Geo))
}

// ============================================================
// RADIO
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

	// Загрузка глобального block.json
	var banned map[string]bool
	if blockData, err := block.LoadBlockList("block.json"); err == nil {
		banned = blockData
		fmt.Printf("✅ Block list loaded: %d banned addresses\n", len(banned))

		// Проверка самого себя
		if block.IsBanned(banned, address) {
			fmt.Println("⛔ Warn! You are banned. Bye.")
			os.Exit(1)
		}
	} else {
		fmt.Printf("⚠️ Block list not loaded: %v\n", err)
	}

	fmt.Println("=== BoomNode v0.3.1-alpha ===")
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

	seeds, err := loadSeeds("seeds.json")
	if err != nil {
    	fmt.Printf("⚠️ Seeds file not loaded: %v\n", err)
	} else {
    	for _, seed := range seeds {
        	meshSrv.AddPeer(seed.Address, seed.UDPAddr, true)
        	fmt.Printf("🌱 Seed peer added: %s\n", seed.Address)
    	}
	}
	
	handler := func(session *boomex.Session, msg *boomex.Message) {
		// Проверка глобального бана
		if banned != nil && block.IsBanned(banned, msg.From) {
			fmt.Printf("⛔ Blocked message from globally banned: %s\n", msg.From)
			return
		}

		// Проверка локального бана в эхе
		if strings.HasPrefix(msg.To, "boombox.") || strings.HasPrefix(msg.To, "emergency.") {
			if echo.IsBannedInEcho(store.DB(), msg.To, msg.From) {
				fmt.Printf("⛔ Blocked message from locally banned: %s in %s\n", msg.From, msg.To)
				return
			}
		}

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

	cfg, err := config.Load("boomnode.yaml")
	if err != nil {
		fmt.Printf("Config error: %v\n", err)
		os.Exit(1)
	}

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
// DIAL-UP
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
// STATUS
// ============================================================

func handleStatusCommand() {
	resp, err := http.Get(apiBase + "/api/status")
	if err == nil {
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		prettyPrintJSON(string(result))
		return
	}

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

// ============================================================
// SEEDS
// ============================================================

type SeedPeer struct {
	Address     string `json:"address"`
	TCP         string `json:"tcp"`
	UDP         string `json:"udp"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
}

func loadSeeds(path string) ([]SeedPeer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seeds []SeedPeer
	if err := json.Unmarshal(data, &seeds); err != nil {
		return nil, err
	}
	return seeds, nil
}
