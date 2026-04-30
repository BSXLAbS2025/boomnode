┌──(glitchy㉿Glitchy-Machine)-[~/boomnode]
└─$ cd ~/boomnode               
go mod download   # скачать зависимости (crypto, yaml, bbolt)
go mod tidy       # почистить go.mod/go.sum
go build -o bn ./cmd/boomnode   # собрать бинарник
./bn run          # запустить
Хранилище открыто: data/boomnode.db
=== BoomNode v0.1.0-alpha ===
BoomNet: Where Ideas Detonate

Адрес:     BM-RU-6TE3QA5U
Имя узла:  BSX Station
Хранилище: ./data

Узел запущен. Ожидание подключений...
(Нажми Ctrl+C для выхода)
