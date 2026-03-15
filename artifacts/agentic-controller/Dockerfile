FROM python:3.12-slim

WORKDIR /app

# Install uv
RUN pip install --no-cache-dir uv

# Copy dependency files
COPY pyproject.toml uv.lock .python-version README.md ./

# Sync dependencies (exclude project install for caching)
RUN uv sync --frozen --no-cache --no-install-project

# Copy source code
COPY src/ src/

# Sync project (this installs the local package)
RUN uv sync --frozen --no-cache

EXPOSE 8080

# Use the binary directly from the venv to avoid runtime sync
CMD ["/app/.venv/bin/kagent-adk", "run", "src", "--host", "0.0.0.0", "--port", "8080"]



