# --------------------------------------------------------------------------
# Stage 1 – builder: install dependencies into a virtual environment
# --------------------------------------------------------------------------
FROM python:3.12-slim AS builder

WORKDIR /build

COPY requirements.txt .
RUN pip install --no-cache-dir --upgrade pip \
 && pip install --no-cache-dir --prefix=/install -r requirements.txt

# --------------------------------------------------------------------------
# Stage 2 – runtime: minimal non-root image
# --------------------------------------------------------------------------
FROM python:3.12-slim AS runtime

LABEL org.opencontainers.image.title="kagent-event-watcher" \
      org.opencontainers.image.description="Kubernetes Event → kagent A2A bridge" \
      org.opencontainers.image.version="1.0.0"

# Create a non-root user
RUN addgroup --system watcher && adduser --system --ingroup watcher watcher

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /install /usr/local

# Copy the application
COPY watcher.py .

USER watcher

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

ENTRYPOINT ["python", "watcher.py"]
