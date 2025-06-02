module github.com/LerianStudio/gomcp-sdk/examples/database

go 1.23.0

require (
	github.com/LerianStudio/gomcp-sdk v0.0.0-20250526191326-79829d2481cb
	github.com/go-sql-driver/mysql v1.8.1
	github.com/lib/pq v1.10.9
	github.com/mattn/go-sqlite3 v1.14.22
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
)

replace github.com/LerianStudio/gomcp-sdk => ../..
