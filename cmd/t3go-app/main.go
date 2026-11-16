package main

import (
	asukaapp "github.com/egoist/asuka/app"
	"github.com/egoist/asuka/ui"
)

func main() {
	asukaapp.RunWindow(ui.WindowOptions{Title: ui.Val("T3Go"), Width: 1100, Height: 760, MinWidth: 420, MinHeight: 480}, Root)
}
