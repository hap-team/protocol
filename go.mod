module github.com/hap-team/protocol

go 1.26.0

require (
	github.com/hap-team/receipts/go v0.0.0
	github.com/santhosh-tekuri/jsonschema/v5 v5.3.1
	gopkg.in/yaml.v3 v3.0.1
	nhooyr.io/websocket v1.8.17
)

// Local source integration; pin the released receipt module before publication.
replace github.com/hap-team/receipts/go => ../receipts/go
