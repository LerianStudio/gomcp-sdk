module github.com/fredcamaral/gomcp-sdk/examples/calculator

go 1.23.0

require github.com/fredcamaral/gomcp-sdk v0.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
)

replace github.com/fredcamaral/gomcp-sdk => ../..
