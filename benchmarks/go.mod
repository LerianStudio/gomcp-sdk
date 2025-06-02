module benchmarks

go 1.23.0

toolchain go1.24.3

replace github.com/LerianStudio/gomcp-sdk => ..

require (
	github.com/LerianStudio/gomcp-sdk v0.0.0-00010101000000-000000000000
	github.com/gorilla/websocket v1.5.3
)

require (
	github.com/golang-jwt/jwt/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
)
