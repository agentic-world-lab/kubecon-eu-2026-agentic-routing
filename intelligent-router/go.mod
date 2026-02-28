module github.com/antonioberben/intelligent-router

go 1.24.1

require (
	github.com/envoyproxy/go-control-plane/envoy v1.32.4
	github.com/prometheus/client_golang v1.20.5
	github.com/vllm-project/semantic-router/candle-binding v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.70.0
	gopkg.in/yaml.v2 v2.4.0
)

// candle-binding is a local module that wraps the Rust candle ML library via CGO.
// It is only compiled when CGO_ENABLED=1 (see Dockerfile.ml and mlclassifier.go).
// The pure-Go build (CGO_ENABLED=0) uses the mlclassifier_stub.go no-op instead.
replace github.com/vllm-project/semantic-router/candle-binding => ../candle-binding

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20240905190251-b4127c9b8d78 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241202173237-19429a94021a // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)
