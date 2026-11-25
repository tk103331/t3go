package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
)

// updateState is the Settings → About update card's state. MyGo installs
// signed updates of the packaged app; development builds have none.
type updateState struct {
	probed     bool
	enabled    bool
	checking   bool
	installing bool
	available  *mygo.Update
	note       string
	err        string
	downloaded int64
	total      int64
}

// updatesEnabled reports whether this build can replace itself. Enabled
// probes where the app is installed, so cache the answer.
func (s *Session) updatesEnabled() bool {
	if !s.updates.probed {
		s.updates.enabled = mygo.Updater.Enabled()
		s.updates.probed = true
	}
	return s.updates.enabled
}

// checkForUpdates asks the signed update feed for a newer version.
func (s *Session) checkForUpdates() {
	u := &s.updates
	if u.checking || u.installing || !s.updatesEnabled() {
		return
	}
	u.checking, u.note, u.err, u.available = true, "", "", nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		update, err := mygo.Updater.Check(ctx)
		s.post(func() {
			u.checking = false
			switch {
			case err != nil && errors.Is(err, mygo.ErrUpdatesDisabled):
				// The disabled card already says why there is nothing to do.
			case err != nil:
				u.err = friendlyError(err)
			case update == nil:
				u.note = i18n.T("You're up to date.")
			default:
				u.available = update
			}
		})
	}()
}

// installUpdate downloads and installs the update, then restarts the app.
func (s *Session) installUpdate() {
	u := &s.updates
	if u.installing || u.available == nil {
		return
	}
	update := u.available
	u.installing, u.err, u.downloaded, u.total = true, "", 0, 0
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := update.Install(ctx, func(downloaded, total int64) {
			s.post(func() {
				u.downloaded, u.total = downloaded, total
			})
		})
		s.post(func() {
			u.installing = false
			if err != nil {
				u.err = friendlyError(err)
				return
			}
			// On macOS the bundle is replaced once the app exits, which
			// Relaunch asks for; elsewhere the files are already in place.
			u.note = i18n.T("Update installed. Restarting…")
			mygo.App.Relaunch()
		})
	}()
}

// updateCard is the Settings → About client update control.
func (s *Session) updateCard(c *ui.Context) {
	t := c.Theme()
	u := &s.updates
	enabled := s.updatesEnabled()
	ui.Column(c).Gap(7).Padding(9, 11).Radius(8).Border(1, t.Border).
		Background(t.Surface).Children(func() {
		text(c, i18n.T("Client updates"), 12.5, t.Text, 600)
		switch {
		case u.installing:
			faint(c, i18n.T("Installing update…"), 11.5)
			if u.total > 0 {
				percent := int(float64(u.downloaded) / float64(u.total) * 100)
				faint(c, fmt.Sprintf("%d%% · %s / %s", min(percent, 100), humanSize(u.downloaded), humanSize(u.total)), 11)
			}
		case u.available != nil:
			faint(c, i18n.T("T3Go %s is available.", u.available.Version), 11.5)
			if notes := strings.TrimSpace(u.available.Notes); notes != "" {
				ui.Text(c, notes).FontSize(10.5).FixedLineHeight(15).TextColor(t.TextMuted).MaxLines(6)
			}
			ui.PrimaryButton(c, i18n.T("Install update")).OnClick(s.installUpdate)
		default:
			ui.Button(c, i18n.T("Check for updates")).Disabled(u.checking || !enabled).
				OnClick(s.checkForUpdates)
			if u.checking {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(12, 12).Label(i18n.T("Checking for updates"))
					faint(c, i18n.T("Checking for updates…"), 11.5)
				})
			}
			if u.note != "" {
				faint(c, u.note, 11.5)
			}
		}
		if u.err != "" {
			errorNote(c, u.err)
		}
		if !enabled {
			faint(c, i18n.T("Updates are available in installed builds. Development builds update by rebuilding."), 11)
		}
	})
}
