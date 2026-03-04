# Use a slim Python image
FROM python:3.12-slim

# Install uv for fast dependency management
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

# Set working directory
WORKDIR /app

# Copy project files
COPY pyproject.toml README.md ./
COPY src/ ./src/

# Install the project and dependencies
RUN uv pip install --system .

# Set environment variables
# Note: ESIOS_API_KEY should be provided at runtime
ENV ESIOS_API_KEY="request_your_personal_token_sending_email_to_consultasios@ree.es"
ENV PYTHONUNBUFFERED=1

# MCP servers communicate via stdio by default.
# To run the server:
ENTRYPOINT ["python", "-m", "electricity_price_mcp.server"]
