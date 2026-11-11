package main

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed terminal_assets/xterm.js
var xtermJS string

//go:embed terminal_assets/xterm.css
var xtermCSS string

//go:embed terminal_assets/addon-fit.js
var xtermFitJS string

// terminalHTML is the page the terminal surface loads: xterm.js wired to
// the environment's terminal WebSocket.
func (a *app) terminalHTML(sessionID string) string {
	c := a.client()
	if c == nil {
		return ""
	}
	quoted, _ := json.Marshal(c.TerminalURL(sessionID))
	page := strings.NewReplacer(
		"/*XTERM_CSS*/", xtermCSS,
		"/*XTERM_JS*/", xtermJS,
		"/*FIT_JS*/", xtermFitJS,
		"__WS_URL__", string(quoted),
	).Replace(terminalPage)
	return page
}

const terminalPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>/*XTERM_CSS*/</style>
<style>
  html,body{margin:0;padding:0;height:100%;background:#1e1e1e;overflow:hidden}
  #term{position:absolute;top:4px;right:2px;bottom:2px;left:6px}
  .xterm .xterm-viewport{background:#1e1e1e !important;scrollbar-width:thin;
    scrollbar-color:rgba(255,255,255,.28) transparent}
  .xterm .xterm-viewport::-webkit-scrollbar{width:10px}
  .xterm .xterm-viewport::-webkit-scrollbar-track{background:transparent}
  .xterm .xterm-viewport::-webkit-scrollbar-thumb{background:rgba(255,255,255,.28);
    border:2px solid #1e1e1e;border-radius:5px}
  .xterm .xterm-viewport::-webkit-scrollbar-thumb:hover{background:rgba(255,255,255,.45)}
</style>
</head>
<body>
<div id="term"></div>
<script>/*XTERM_JS*/</script>
<script>/*FIT_JS*/</script>
<script>
(function () {
  const term = new Terminal({
    cursorBlink: true,
    cursorStyle: 'bar',
    fontSize: 12,
    lineHeight: 1.2,
    fontFamily: 'Menlo, Monaco, "SF Mono", monospace',
    scrollback: 5000,
    allowTransparency: false,
    theme: {
      background: '#1e1e1e',
      foreground: '#d4d4d4',
      cursor: '#aeafad',
      selectionBackground: '#264f78',
      black: '#000000', red: '#cd3131', green: '#0dbc79', yellow: '#e5e510',
      blue: '#2472c8', magenta: '#bc3fbc', cyan: '#11a8cd', white: '#e5e5e5',
      brightBlack: '#666666', brightRed: '#f14c4c', brightGreen: '#23d18b',
      brightYellow: '#f5f543', brightBlue: '#3b8eea', brightMagenta: '#d670d6',
      brightCyan: '#29b8db', brightWhite: '#e5e5e5'
    }
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('term'));

  const ws = new WebSocket(__WS_URL__);
  let ready = false;
  function sendResize() {
    if (ready) ws.send(JSON.stringify({type: 'resize', cols: term.cols, rows: term.rows}));
  }
  function refit() {
    try { fit.fit(); } catch (e) {}
    sendResize();
  }
  ws.onopen = function () {
    ready = true;
    requestAnimationFrame(function () { refit(); term.focus(); });
  };
  ws.onmessage = function (event) {
    let message;
    try { message = JSON.parse(event.data); } catch (e) { return; }
    if (message.type === 'output' && message.data) term.write(message.data);
    if (message.type === 'exit') {
      term.write('\r\n\x1b[38;5;244m[process exited ' + message.code + ']\x1b[0m\r\n');
      try { if (window.nsgo) window.nsgo.postMessage('exit'); } catch (e) {}
    }
  };
  ws.onclose = function () {
    ready = false;
    term.write('\r\n\x1b[38;5;244m[terminal disconnected]\x1b[0m\r\n');
  };
  term.onData(function (data) {
    if (ready) ws.send(JSON.stringify({type: 'input', data: data}));
  });
  term.onResize(sendResize);
  new ResizeObserver(function () { refit(); }).observe(document.getElementById('term'));
  window.addEventListener('resize', refit);
  setTimeout(refit, 60);
})();
</script>
</body>
</html>
`
