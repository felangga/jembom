# --- Tahap 1: Builder ---
FROM golang:1-bookworm as builder

WORKDIR /usr/src/app

# Wajib untuk driver SQLite: Pastikan CGO aktif
ENV CGO_ENABLED=1
ENV GOOS=linux

# Salin file module dan download dependency terlebih dahulu (untuk caching)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Salin seluruh source code
COPY . .

# Build aplikasi dengan flag -ldflags="-s -w" agar ukuran binary lebih kecil
RUN go build -v -ldflags="-s -w" -o /run-app ./cmd/server


# --- Tahap 2: Runtime ---
FROM debian:bookworm-slim

# Install sertifikat keamanan (untuk HTTPS) dan zona waktu
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Buat direktori khusus untuk menyimpan database SQLite
RUN mkdir -p /app/data

# Copy file binary hasil build dari tahap 1
COPY --from=builder /run-app /app/run-app

# Beri tahu Docker bahwa folder ini adalah Volume
VOLUME ["/app/data"]

# Eksekusi aplikasi
CMD ["/app/run-app"]
