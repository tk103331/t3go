package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// Render the real message view: a one-line message must stay one line
// high, and wrapping must add lines without overflowing a phone's width.
func TestUIUserMessageLayout(t *testing.T) {
	for _, width := range []int{360, 900} {
		for _, tc := range []struct {
			name    string
			content string
			minH    float32
			maxH    float32
		}{
			{"single line", "hello", 19, 19},
			{"two lines", "hello\nworld", 38, 38},
			{"wrapped", strings.Repeat("请保留文件、终端和消息操作。", 12), 38, 380},
		} {
			t.Run(fmt.Sprintf("%d/%s", width, tc.name), func(t *testing.T) {
				s := &Session{}
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Padding(16).Children(func() {
						s.renderMessage(c, proto.Message{ID: "layout", Role: proto.RoleUser,
							Kind: proto.KindText, Content: tc.content, CreatedAt: time.Now()})
					})
				}, width, 700)
				tt.Frame()
				body, ok := tt.Find(tc.content)
				if !ok || body.H < tc.minH || body.H > tc.maxH {
					t.Fatalf("message height: %+v, want %.0f..%.0f", body, tc.minH, tc.maxH)
				}
				if body.X < 16 || body.X+body.W > float32(width-16)+1 {
					t.Fatalf("message overflows its viewport: %+v", body)
				}
				for _, label := range []string{"Copy message", "Edit message", "Branch from here"} {
					if _, ok := tt.Access(i18n.T(label)); !ok {
						t.Fatalf("message action missing: %s", label)
					}
				}
			})
		}
	}
}
