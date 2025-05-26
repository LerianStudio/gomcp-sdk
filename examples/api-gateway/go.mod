module github.com/fredcamaral/gomcp-sdk/examples/api-gateway

go 1.23.0

require (
	github.com/fredcamaral/gomcp-sdk v0.0.0-20250526191326-79829d2481cb
	golang.org/x/time v0.5.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
)

replace github.com/fredcamaral/gomcp-sdk => ../..
