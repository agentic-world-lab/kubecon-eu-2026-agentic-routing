# Spain Electricity Price MCP

An MCP server implemented with `FastMCP` that provides the current price of electricity in Spain (PVPC 2.0TD tariff).

## Features
- **get_current_price**: Fetches the current electricity price in €/MWh for the current hour in Spain (Peninsula).

## Installation
Requires Python 3.10+.

```bash
uv pip install .
```

## Configuration
You need an ESIOS API key from Red Eléctrica Española. You can request one by emailing `consultasios@ree.es`.

Set the API key as an environment variable:
```bash
export ESIOS_API_KEY=your_token_here
```

## Usage
Run the server:
```bash
python -m electricity_price_mcp.server
```
or use the entry point:
```bash
electricity-price
```

## Docker Usage

To build the image:
```bash
docker build -t electricity-price-mcp .
```

To run the container and expose it to an agent (via stdio):
```bash
docker run -i --rm -e ESIOS_API_KEY=your_token_here electricity-price-mcp
```

If you want to use the SSE (Server-Sent Events) transport instead of stdio (e.g., for a remote agent):
```bash
docker run -p 8000:8000 -e ESIOS_API_KEY=your_token_here electricity-price-mcp python -m electricity_price_mcp.server sse
```

## Implementation Details
- Uses `FastMCP` framework.
- Queries indicator `1001` from ESIOS API.
- Handles `Europe/Madrid` timezone correctly.
- Best practices follow the Model Context Protocol standards.
