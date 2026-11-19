package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/tk/t3go/internal/i18n"
)

// TODO(view): scanEnvironmentQR opens the system camera (NSGo's
// TakePhoto), decodes the capture with pairingLinkFromQRImage below, and
// fills the connection form with the pairing link. MyGo has no camera API
// wired up yet; the view will drive it.
func (s *Session) scanEnvironmentQR() {
	s.connErr = ""
	// TODO(view): camera capture is not available in MyGo yet; enter the
	// pairing link by hand.
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
