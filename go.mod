module github.com/pencode-labs/markdown-preview

go 1.27.0

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/onix-engine/onix v0.1.10
	github.com/pencode-labs/pencode v0.1.0-rc.15
	github.com/yuin/goldmark/v2 v2.1.16
	github.com/zkrebbekx/go-mermaid v0.1.6
	golang.org/x/net v0.63.0
)

require (
	github.com/dlclark/regexp2/v2 v2.13.2 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

exclude github.com/onix-engine/onix v0.0.0-00010101000000-000000000000

replace github.com/onix-engine/onix => ../../../onix
