package node

// previewPage: a button that renames the page, a field that does too as it is typed in, and a link elsewhere.
const previewPage = `<!doctype html><html><head><title>Hi</title><style>
body { margin: 0; background: #00f } #go { position: absolute; left: 0; top: 0; width: 100px; height: 50px }
#in { position: absolute; left: 0; top: 100px; width: 200px; height: 40px }
#ext { position: absolute; left: 0; top: 200px; width: 200px; height: 40px; display: block }
#next { position: absolute; left: 0; top: 300px; width: 200px; height: 40px; display: block }
</style></head><body>
<button id="go" onclick="document.title='clicked'">Go</button>
<input id="in" oninput="document.title='v:'+this.value">
<a id="ext" href="https://example.com/away">elsewhere</a>
<a id="next" href="/second">second</a>
</body></html>`

type liveMsg struct {
	kind byte
	json map[string]any
	bin  []byte
}
