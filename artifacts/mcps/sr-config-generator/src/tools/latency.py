"""Latency tool for MCP server.
"""

from core.server import mcp
from core.utils import get_tool_config


@mcp.tool()
def latency(message: str) -> str:
    """Latency tool implementation.

    This is a template function. Replace this implementation with your tool logic.

    Args:
        message: Input message (replace with your actual parameters)

    Returns:
        str: Result of the tool operation (replace with your actual return type)
    """
    # Get tool-specific configuration from kmcp.yaml
    config = get_tool_config("latency")

    # TODO: Replace this basic implementation with your tool logic

    # Example: Basic text processing
    prefix = config.get("prefix", "echo: ")
    return f"{prefix}{message}"
