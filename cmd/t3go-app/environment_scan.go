package main

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
	"github.com/oneo-me/nsgo/core"
)

// scanEnvironmentQR uses the system camera and decodes the captured image
// locally. Pairing credentials never leave the device during scanning.
func (a *app) scanEnvironmentQR(cx *core.Scope) {
	a.connErr.Set("")
	cx.TakePhoto(core.PhotoOptions{MaxDimension: 2400, Quality: 0.95}, func(photo *core.PickedFile, err error) {
		if err != nil {
			if errors.Is(err, core.ErrNoCamera) {
				a.connErr.Set("This device has no camera available. Enter the pairing link instead.")
			} else {
				a.connErr.Set("Could not open the camera: " + err.Error())
			}
			return
		}
		if photo == nil {
			return
		}
		link, err := pairingLinkFromQRImage(photo.Data)
		if err != nil {
			a.connErr.Set(err.Error())
			return
		}
		a.serverURL.Set("")
		a.token.Set(link)
		a.connect("", link)
	})
}

// pairingLinkFromQRImage finds the first T3Go pair link in a captured image.
func pairingLinkFromQRImage(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("The camera returned an empty image. Try again.")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("The captured image could not be read. Try again.")
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("Could not prepare the QR code: %w", err)
	}
	result, err := qrcode.NewQRCodeReader().DecodeWithoutHints(bitmap)
	if err != nil {
		return "", errors.New("No readable T3Go pairing QR code found. Make sure the whole code is visible and try again.")
	}
	link := strings.TrimSpace(result.GetText())
	link = strings.TrimPrefix(link, "#")
	if strings.HasPrefix(link, "t3go://pair?") {
		return link, nil
	}
	return "", errors.New("This is not a T3Go pairing QR code.")
}
