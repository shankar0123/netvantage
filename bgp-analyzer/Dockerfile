FROM python:3.12-slim AS builder

WORKDIR /app

# Install system dependencies for pybgpstream.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        librdkafka-dev \
    && rm -rf /var/lib/apt/lists/*

COPY pyproject.toml .
COPY src/ src/

RUN pip install --no-cache-dir .

FROM python:3.12-slim

WORKDIR /app

# Runtime dependencies for pybgpstream.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        librdkafka1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin
COPY src/ src/

EXPOSE 9100

ENTRYPOINT ["python", "-m", "netvantage_bgp"]
CMD ["--metrics-port", "9100"]
