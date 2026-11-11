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
	return terminalPageFor(c.TerminalURL(sessionID), a.cx.Dark())
}

// terminalTheme is the terminal's colors, following the app's appearance.
type terminalTheme struct {
	Background          string `json:"background"`
	Foreground          string `json:"foreground"`
	Cursor              string `json:"cursor"`
	SelectionBackground string `json:"selectionBackground"`
	Black               string `json:"black"`
	Red                 string `json:"red"`
	Green               string `json:"green"`
	Yellow              string `json:"yellow"`
	Blue                string `json:"blue"`
	Magenta             string `json:"magenta"`
	Cyan                string `json:"cyan"`
	White               string `json:"white"`
	BrightBlack         string `json:"brightBlack"`
	BrightRed           string `json:"brightRed"`
	BrightGreen         string `json:"brightGreen"`
	BrightYellow        string `json:"brightYellow"`
	BrightBlue          string `json:"brightBlue"`
	BrightMagenta       string `json:"brightMagenta"`
	BrightCyan          string `json:"brightCyan"`
	BrightWhite         string `json:"brightWhite"`
}

var terminalDark = terminalTheme{
	Background: "#1e1e1e", Foreground: "#d4d4d4", Cursor: "#aeafad", SelectionBackground: "#264f78",
	Black: "#000000", Red: "#cd3131", Green: "#0dbc79", Yellow: "#e5e510", Blue: "#2472c8",
	Magenta: "#bc3fbc", Cyan: "#11a8cd", White: "#e5e5e5",
	BrightBlack: "#666666", BrightRed: "#f14c4c", BrightGreen: "#23d18b", BrightYellow: "#f5f543",
	BrightBlue: "#3b8eea", BrightMagenta: "#d670d6", BrightCyan: "#29b8db", BrightWhite: "#e5e5e5",
}

var terminalLight = terminalTheme{
	Background: "#ffffff", Foreground: "#24292f", Cursor: "#0969da", SelectionBackground: "#b6d7ff",
	Black: "#24292f", Red: "#cf222e", Green: "#116329", Yellow: "#9a6700", Blue: "#0969da",
	Magenta: "#8250df", Cyan: "#1b7c83", White: "#6e7781",
	BrightBlack: "#57606a", BrightRed: "#a40e26", BrightGreen: "#1a7f37", BrightYellow: "#7d4e00",
	BrightBlue: "#218bff", BrightMagenta: "#a475f9", BrightCyan: "#3192aa", BrightWhite: "#8c959f",
}

// terminalPageFor renders the terminal page for one appearance.
func terminalPageFor(wsURL string, dark bool) string {
	theme := terminalLight
	scroll, scrollHover := "rgba(0,0,0,.25)", "rgba(0,0,0,.4)"
	if dark {
		theme = terminalDark
		scroll, scrollHover = "rgba(255,255,255,.28)", "rgba(255,255,255,.45)"
	}
	quoted, _ := json.Marshal(wsURL)
	themeJSON, _ := json.Marshal(theme)
	return strings.NewReplacer(
		"/*XTERM_CSS*/", xtermCSS,
		"/*XTERM_JS*/", xtermJS,
		"/*FIT_JS*/", xtermFitJS,
		"__WS_URL__", string(quoted),
		"__THEME__", string(themeJSON),
		"__BG__", theme.Background,
		"__SCROLL__", scroll,
		"__SCROLL_HOVER__", scrollHover,
	).Replace(terminalPage)
}

const terminalPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>/*XTERM_CSS*/</style>
<style>
  html,body{margin:0;padding:0;height:100%;background:__BG__;overflow:hidden}
  #term{position:absolute;top:4px;right:2px;bottom:2px;left:6px}
  .xterm .xterm-viewport{background:__BG__ !important;scrollbar-width:thin;
    scrollbar-color:__SCROLL__ transparent}
  .xterm .xterm-viewport::-webkit-scrollbar{width:10px}
  .xterm .xterm-viewport::-webkit-scrollbar-track{background:transparent}
  .xterm .xterm-viewport::-webkit-scrollbar-thumb{background:__SCROLL__;
    border:2px solid __BG__;border-radius:5px}
  .xterm .xterm-viewport::-webkit-scrollbar-thumb:hover{background:__SCROLL_HOVER__}
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
    theme: __THEME__
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
