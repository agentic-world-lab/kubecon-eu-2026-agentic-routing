import pytest
from electricity_price_mcp.server import ElectricityPriceMCP
from mcp.server.fastmcp import FastMCP

import asyncio

def test_mcp_initialization():
    """Test that the MCP initializes correctly and registers the tool."""
    server = ElectricityPriceMCP(api_key="test_key")
    assert server.api_key == "test_key"
    tools = asyncio.run(server.list_tools())
    assert "get_current_price" in [tool.name for tool in tools]

def test_default_api_key():
    """Test that the default API key is correctly set."""
    server = ElectricityPriceMCP()
    assert server.api_key == "request_your_personal_token_sending_email_to_consultasios@ree.es"

def test_inheritance():
    """Test that ElectricityPriceMCP inherits from FastMCP (via BaseMCP)."""
    server = ElectricityPriceMCP()
    assert isinstance(server, FastMCP)
