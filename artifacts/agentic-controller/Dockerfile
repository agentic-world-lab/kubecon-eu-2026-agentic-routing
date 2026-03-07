FROM python:3.13-slim

WORKDIR /app

# Install uv for dependency management
COPY pyproject.toml .python-version README.md ./
RUN pip install --no-cache-dir uv && \
    uv venv && \
    uv pip install --no-cache -r pyproject.toml

COPY src/ src/
COPY main.py .

EXPOSE 8080

CMD ["uv", "run", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8080"]
