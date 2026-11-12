package server

import (
	"net"
	"net/http"
	"strconv"

	"github.com/tk/t3go/internal/preview"
	"github.com/tk/t3go/internal/proto"
)

// handlePreviewServers lists the local development servers this machine
// is listening on, without T3Go's own port.
func (s *Server) handlePreviewServers(w http.ResponseWriter, r *http.Request) {
	servers, err := preview.List(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"servers": []any{}, "detail": err.Error()})
		return
	}
	own := ownPort(s.Config.Listen)
	out := make([]proto.PreviewServer, 0, len(servers))
	for _, server := range servers {
		if server.Port == own || server.Port < 1024 {
			continue
		}
		out = append(out, server)
	}
	writeJSON(w, 200, map[string]any{"servers": out})
}

// ownPort reads the port out of an address as 127.0.0.1:4870 or :4870.
func ownPort(listen string) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(port)
	return value
}
