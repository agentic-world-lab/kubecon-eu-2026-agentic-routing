# Route Picker Server

This is a simple server application that provides route picking functionality.

## What It Does

This external processing service implements Envoy's External Processing filter interface. It takes instructions in a request or response header named `instructions` and can:
- Add headers to requests/responses
- Remove headers from requests/responses
- Set body content
- Set trailers

The `instructions` header must be a JSON string in this format:

```json
{
  "addHeaders": {
    "header1": "value1",
    "header2": "value2"
  },
  "removeHeaders": ["header3", "header4"],
  "setBody": "this is the new body",
  "setTrailers": {
    "trailer1": "value1",
    "trailer2": "value2"
  }
}
```

All fields are optional.

## Run the server

```bash
# download deps (optional; go will do it automatically)
go mod tidy
go mod download


# run directly (dev)
ROUTING_POLICY_FILE=policies/policy.rego DEFAULT_ROUTE=cpu go run . -grpcport :18080
```

## Test the server

```bash
go build -o _output/testclient ./cmd/testclient
```

```bash
./_output/testclient -addr localhost:18080
```


```text
Received ProcessingResponse: {"requestHeaders":{"response":{"headerMutation":{"setHeaders":[{"header":{"key":"x-routing-decision","rawValue":"Y3B1"}}]}}}}
done
```

```bash
echo 'Y3B1' | base64 --decode
# prints: cpu
```

## Deploy the server

```bash
docker buildx build --push \
                --platform linux/amd64,linux/arm64 \
                -t antonioberben/extproc-agentic-routing:latest .
```