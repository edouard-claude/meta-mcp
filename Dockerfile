# --- build ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/metasocial-mcp ./cmd/metasocial-mcp

# --- run ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/metasocial-mcp /metasocial-mcp
# The SQLite file lives on a persistent volume mounted here.
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/metasocial-mcp"]
