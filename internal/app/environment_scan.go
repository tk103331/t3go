package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

// scanEnvironmentQR reads a pairing code from a picture. This client has
// no camera of its own, so the reader picks the picture — a photograph
// of the code another device shows — and the decoder below reads it.
func (s *Session) scanEnvironmentQR() {
	s.connErr = ""
	if jsRuntime {
		s.connErr = i18n.T("This platform has no camera. Enter the pairing link instead.")
		return
	}
	s.pickPairingImage(func(data []byte, err error) {
		if err != nil {
			s.connErr = i18n.T("Could not open the camera: ") + err.Error()
			return
		}
		link, err := pairingLinkFromQRImage(data)
		if err != nil {
			s.connErr = err.Error()
			return
		}
		s.token = link
		if address, _, ok := client.ParsePairing(link); ok && address != "" {
			s.serverURL = address
		}
		s.connErr = ""
	})
}

// pickPairingImage opens a picture of a pairing code and reads its
// bytes.
func (s *Session) pickPairingImage(done func(data []byte, err error)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: s.win,
			Title:  i18n.T("Choose a pairing QR code"),
		})
		var data []byte
		if err == nil {
			if len(paths) == 0 {
				err = errors.New(i18n.T("No picture was chosen."))
			} else {
				data, err = os.ReadFile(paths[0])
			}
		}
		s.post(func() { done(data, err) })
	}()
}

// pairingLinkFromQRImage finds the first T3Go pair link in a captured image.
func pairingLinkFromQRImage(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New(i18n.T("The camera returned an empty image. Try again."))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New(i18n.T("The captured image could not be read. Try again."))
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("Could not prepare the QR code: %w", err)
	}
	result, err := qrcode.NewQRCodeReader().DecodeWithoutHints(bitmap)
	if err != nil {
		return "", errors.New(i18n.T("No readable T3Go pairing QR code found. Make sure the whole code is visible and try again."))
	}
	link := strings.TrimSpace(result.GetText())
	link = strings.TrimPrefix(link, "#")
	if strings.HasPrefix(link, "t3go://pair?") {
		return link, nil
	}
	return "", errors.New(i18n.T("This is not a T3Go pairing QR code."))
}
