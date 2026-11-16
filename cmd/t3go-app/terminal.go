package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/oneo-me/nsgo/core"
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
	// Rebuild the WebView source when the same environment changes route,
	// keeping the selected remote terminal rather than creating another shell.
	_ = a.serverURL.Get()
	c := a.client()
	if c == nil {
		return ""
	}
	// The route and session identify the bridge page. Changing either
	// reloads it and asks the Go bridge to attach the new WebSocket.
	return terminalPageFor(c.BaseURL+"#"+sessionID, a.cx.Dark())
}

// The page has no network origin: xterm talks to the Go bridge, which owns
// the WebSocket. Android blocks cleartext ws:// from WebViews with this
// app's target SDK, while the native Go client is unaffected.
func (a *app) terminalSource(sessionID string) core.WebSource {
	return core.WebHTML(a.terminalHTML(sessionID), "")
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
		"__BRIDGE_ROUTE__", string(quoted),
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
  #reconnect{display:none;position:absolute;right:12px;top:8px;z-index:2;
    min-height:44px;padding:0 14px;border:1px solid #888;border-radius:8px;
    background:__BG__;color:inherit;cursor:pointer}
</style>
</head>
<body>
<div id="term"></div>
<button id="reconnect" type="button">Reconnect</button>
<script>/*XTERM_JS*/</script>
<script>/*FIT_JS*/</script>
<script>
(function () {
  const bridgeRoute = __BRIDGE_ROUTE__;
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

  let timer, attempts = 0, ready = false, exited = false, disposed = false;
  const reconnect = document.getElementById('reconnect');
  function post(message) {
    if (window.nsgo) window.nsgo.postMessage(JSON.stringify(message));
  }
  function sendResize() {
    if (ready) post({type: 'resize', cols: term.cols, rows: term.rows});
  }
  function refit() {
    try { fit.fit(); } catch (e) {}
    sendResize();
  }
  function retry() {
    if (disposed || exited || timer || attempts >= 5) return;
    timer = setTimeout(function () { timer = null; connect(); }, Math.min(500 * Math.pow(2, attempts), 8000));
    attempts++;
  }
  function disconnected() {
    if (disposed || exited) return;
    ready = false;
    reconnect.style.display = 'block';
    term.write('\r\n\x1b[38;5;244m[terminal disconnected; retrying connection]\x1b[0m\r\n');
    retry();
  }
  function connect() {
    if (disposed || exited || timer) return;
    if (!window.nsgo) {
      timer = setTimeout(function () { timer = null; connect(); }, 50);
      return;
    }
    post({type: 'ready', route: bridgeRoute});
  }
  function connected() {
    ready = true;
    attempts = 0;
    clearTimeout(timer); timer = null;
    reconnect.style.display = 'none';
    // The server replays its snapshot on every attachment.
    term.reset();
    requestAnimationFrame(function () { refit(); term.focus(); });
  }
  reconnect.onclick = function () {
    clearTimeout(timer); timer = null; attempts = 0;
    connect();
  };
  window.addEventListener('message', function (event) {
    let message;
    try { message = JSON.parse(event.data); } catch (e) { return; }
    if (message.type === 'bridge') {
      if (message.state === 'connected') connected();
      if (message.state === 'disconnected') disconnected();
      return;
    }
    if (message.type === 'output' && message.data) term.write(message.data);
    if (message.type === 'exit') {
      exited = true;
      ready = false;
      clearTimeout(timer); timer = null;
      reconnect.style.display = 'none';
      term.write('\r\n\x1b[38;5;244m[process exited ' + message.code + ']\x1b[0m\r\n');
      post({type: 'exit', code: message.code});
    }
  });
  window.addEventListener('pagehide', function () {
    disposed = true; ready = false; clearTimeout(timer);
    post({type: 'close'});
  });
  connect();
  term.onData(function (data) {
    if (ready) post({type: 'input', data: data});
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

// terminalBridge owns the native terminal WebSocket. The xterm page receives
// server frames through Ref.PostMessage and sends input back through
// window.nsgo.postMessage. This avoids Android WebView's cleartext policy.
type terminalBridge struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	generation uint64
	sessionID  string
	cancel     context.CancelFunc
	conn       *websocket.Conn
}

func newTerminalBridge() *terminalBridge {
	return &terminalBridge{}
}

func (b *terminalBridge) connect(emit func(string), sessionID, rawURL string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	if b.conn != nil {
		_ = b.conn.CloseNow()
	}
	b.generation++
	generation := b.generation
	b.sessionID = sessionID
	b.cancel = cancel
	b.conn = nil
	b.mu.Unlock()

	go func() {
		defer cancel()
		dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
		conn, resp, err := websocket.Dial(dialCtx, rawURL, nil)
		dialCancel()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			b.finish(generation, emit)
			return
		}
		conn.SetReadLimit(1 << 20)
		b.mu.Lock()
		if b.generation != generation {
			b.mu.Unlock()
			_ = conn.CloseNow()
			return
		}
		b.conn = conn
		b.mu.Unlock()
		emitJSON(emit, map[string]any{"type": "bridge", "state": "connected"})
		for {
			typ, data, readErr := conn.Read(ctx)
			if readErr != nil {
				break
			}
			if typ == websocket.MessageText {
				emit(string(data))
			}
		}
		_ = conn.CloseNow()
		b.finish(generation, emit)
	}()
}

func (b *terminalBridge) finish(generation uint64, emit func(string)) {
	b.mu.Lock()
	if b.generation != generation {
		b.mu.Unlock()
		return
	}
	b.conn = nil
	b.cancel = nil
	b.mu.Unlock()
	emitJSON(emit, map[string]any{"type": "bridge", "state": "disconnected"})
}

func (b *terminalBridge) send(sessionID string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	b.mu.Lock()
	conn := b.conn
	matches := conn != nil && b.sessionID == sessionID
	b.mu.Unlock()
	if !matches {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return conn.Write(ctx, websocket.MessageText, data) == nil
}

func (b *terminalBridge) close() {
	b.mu.Lock()
	b.generation++
	cancel := b.cancel
	conn := b.conn
	b.cancel, b.conn, b.sessionID = nil, nil, ""
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.CloseNow()
	}
}

func emitJSON(emit func(string), value any) {
	data, err := json.Marshal(value)
	if err == nil {
		emit(string(data))
	}
}

func (a *app) terminalPageMessage(cx *core.Scope, ref *core.Ref, sessionID, data string) {
	var message struct {
		Type string `json:"type"`
		Data string `json:"data"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if json.Unmarshal([]byte(data), &message) != nil {
		return
	}
	switch message.Type {
	case "ready":
		c := a.client()
		if c == nil {
			return
		}
		emit := func(value string) {
			cx.Post(func() { ref.PostMessage(value) })
		}
		a.terminalBridge.connect(emit, sessionID, c.TerminalURL(sessionID))
	case "input":
		a.terminalBridge.send(sessionID, map[string]any{"type": "input", "data": message.Data})
	case "resize":
		a.terminalBridge.send(sessionID, map[string]any{"type": "resize", "cols": message.Cols, "rows": message.Rows})
	case "exit":
		a.terminalBridge.close()
		a.loadTerminalSessions()
	case "close":
		a.terminalBridge.close()
	}
}
