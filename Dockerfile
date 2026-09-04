# --- build ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/metasocial-mcp ./cmd/metasocial-mcp

# --- run ---
#
# The image runs as root on purpose. The SQLite file lives on a persistent
# volume that the orchestrator creates root-owned, so an unprivileged user
# cannot open it: the process would die on "unable to open database file".
# There is no shell and no package manager in the image, and the only thing
# it runs is this one static binary.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/metasocial-mcp /metasocial-mcp
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/metasocial-mcp"]
