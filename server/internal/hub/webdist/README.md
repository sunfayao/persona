A web client build (`web/dist`) is copied here before building personad when the binary should carry the web client;
this file keeps `//go:embed all:webdist` compiling without one. After a copy,
`go run ./internal/webasset/precompress internal/hub/webdist` keeps its hashed assets and third-party-licenses.txt
gzipped; the first screen's brotli copies (name.br) come from the web build as they are.
