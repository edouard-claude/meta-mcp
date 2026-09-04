module github.com/edouard-claude/meta-mcp

go 1.26.0

// Only two direct dependencies, both required by the SPEC:
//   - go-sdk implements the MCP protocol and its Streamable HTTP transport;
//     writing that by hand would mean re-implementing a moving specification.
//   - modernc.org/sqlite is a pure Go SQLite driver, which keeps the binary
//     static (CGO_ENABLED=0) while still giving us a real database.
// Everything else below is pulled in transitively by these two. Any new
// direct dependency must be justified here before being added.
require (
	github.com/modelcontextprotocol/go-sdk v1.7.0
	modernc.org/sqlite v1.58.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
