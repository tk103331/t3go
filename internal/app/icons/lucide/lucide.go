// Package lucide carries the Lucide icons the client draws, as MyGo
// SVGs. The icons are Lucide (ISC), https://lucide.dev.
package lucide

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

var onceArchive sync.Once

var svgArchive *ui.SVG

// Archive returns the Lucide archive icon.
func Archive() *ui.SVG {
	onceArchive.Do(func() {
		svgArchive = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"20\" height=\"5\" x=\"2\" y=\"3\" rx=\"1\" />\n  <path d=\"M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8\" />\n  <path d=\"M10 12h4\" />\n</svg>\n"))
	})
	return svgArchive
}

var onceArchiveRestore sync.Once

var svgArchiveRestore *ui.SVG

// ArchiveRestore returns the Lucide archive restore icon.
func ArchiveRestore() *ui.SVG {
	onceArchiveRestore.Do(func() {
		svgArchiveRestore = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"20\" height=\"5\" x=\"2\" y=\"3\" rx=\"1\" />\n  <path d=\"M4 8v11a2 2 0 0 0 2 2h2\" />\n  <path d=\"M20 8v11a2 2 0 0 1-2 2h-2\" />\n  <path d=\"m9 15 3-3 3 3\" />\n  <path d=\"M12 12v9\" />\n</svg>\n"))
	})
	return svgArchiveRestore
}

var onceArrowLeft sync.Once

var svgArrowLeft *ui.SVG

// ArrowLeft returns the Lucide arrow left icon.
func ArrowLeft() *ui.SVG {
	onceArrowLeft.Do(func() {
		svgArrowLeft = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m12 19-7-7 7-7\" />\n  <path d=\"M19 12H5\" />\n</svg>\n"))
	})
	return svgArrowLeft
}

var onceArrowRight sync.Once

var svgArrowRight *ui.SVG

// ArrowRight returns the Lucide arrow right icon.
func ArrowRight() *ui.SVG {
	onceArrowRight.Do(func() {
		svgArrowRight = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M5 12h14\" />\n  <path d=\"m12 5 7 7-7 7\" />\n</svg>\n"))
	})
	return svgArrowRight
}

var onceArrowUp sync.Once

var svgArrowUp *ui.SVG

// ArrowUp returns the Lucide arrow up icon.
func ArrowUp() *ui.SVG {
	onceArrowUp.Do(func() {
		svgArrowUp = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m5 12 7-7 7 7\" />\n  <path d=\"M12 19V5\" />\n</svg>\n"))
	})
	return svgArrowUp
}

var onceBot sync.Once

var svgBot *ui.SVG

// Bot returns the Lucide bot icon.
func Bot() *ui.SVG {
	onceBot.Do(func() {
		svgBot = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 8V4H8\" />\n  <rect width=\"16\" height=\"12\" x=\"4\" y=\"8\" rx=\"2\" />\n  <path d=\"M2 14h2\" />\n  <path d=\"M20 14h2\" />\n  <path d=\"M15 13v2\" />\n  <path d=\"M9 13v2\" />\n</svg>\n"))
	})
	return svgBot
}

var onceBrain sync.Once

var svgBrain *ui.SVG

// Brain returns the Lucide brain icon.
func Brain() *ui.SVG {
	onceBrain.Do(func() {
		svgBrain = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 18V5\" />\n  <path d=\"M15 13a4.17 4.17 0 0 1-3-4 4.17 4.17 0 0 1-3 4\" />\n  <path d=\"M17.598 6.5A3 3 0 1 0 12 5a3 3 0 1 0-5.598 1.5\" />\n  <path d=\"M17.997 5.125a4 4 0 0 1 2.526 5.77\" />\n  <path d=\"M18 18a4 4 0 0 0 2-7.464\" />\n  <path d=\"M19.967 17.483A4 4 0 1 1 12 18a4 4 0 1 1-7.967-.517\" />\n  <path d=\"M6 18a4 4 0 0 1-2-7.464\" />\n  <path d=\"M6.003 5.125a4 4 0 0 0-2.526 5.77\" />\n</svg>\n"))
	})
	return svgBrain
}

var onceChartColumn sync.Once

var svgChartColumn *ui.SVG

// ChartColumn returns the Lucide chart column icon.
func ChartColumn() *ui.SVG {
	onceChartColumn.Do(func() {
		svgChartColumn = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M3 3v16a2 2 0 0 0 2 2h16\" />\n  <path d=\"M18 17V9\" />\n  <path d=\"M13 17V5\" />\n  <path d=\"M8 17v-3\" />\n</svg>\n"))
	})
	return svgChartColumn
}

var onceCheck sync.Once

var svgCheck *ui.SVG

// Check returns the Lucide check icon.
func Check() *ui.SVG {
	onceCheck.Do(func() {
		svgCheck = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M20 6 9 17l-5-5\" />\n</svg>\n"))
	})
	return svgCheck
}

var onceChevronDown sync.Once

var svgChevronDown *ui.SVG

// ChevronDown returns the Lucide chevron down icon.
func ChevronDown() *ui.SVG {
	onceChevronDown.Do(func() {
		svgChevronDown = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m6 9 6 6 6-6\" />\n</svg>\n"))
	})
	return svgChevronDown
}

var onceChevronLeft sync.Once

var svgChevronLeft *ui.SVG

// ChevronLeft returns the Lucide chevron left icon.
func ChevronLeft() *ui.SVG {
	onceChevronLeft.Do(func() {
		svgChevronLeft = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m15 18-6-6 6-6\" />\n</svg>\n"))
	})
	return svgChevronLeft
}

var onceChevronRight sync.Once

var svgChevronRight *ui.SVG

// ChevronRight returns the Lucide chevron right icon.
func ChevronRight() *ui.SVG {
	onceChevronRight.Do(func() {
		svgChevronRight = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m9 18 6-6-6-6\" />\n</svg>\n"))
	})
	return svgChevronRight
}

var onceChevronUp sync.Once

var svgChevronUp *ui.SVG

// ChevronUp returns the Lucide chevron up icon.
func ChevronUp() *ui.SVG {
	onceChevronUp.Do(func() {
		svgChevronUp = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m18 15-6-6-6 6\" />\n</svg>\n"))
	})
	return svgChevronUp
}

var onceCircleAlert sync.Once

var svgCircleAlert *ui.SVG

// CircleAlert returns the Lucide circle alert icon.
func CircleAlert() *ui.SVG {
	onceCircleAlert.Do(func() {
		svgCircleAlert = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <line x1=\"12\" x2=\"12\" y1=\"8\" y2=\"12\" />\n  <line x1=\"12\" x2=\"12.01\" y1=\"16\" y2=\"16\" />\n</svg>\n"))
	})
	return svgCircleAlert
}

var onceCircleCheck sync.Once

var svgCircleCheck *ui.SVG

// CircleCheck returns the Lucide circle check icon.
func CircleCheck() *ui.SVG {
	onceCircleCheck.Do(func() {
		svgCircleCheck = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <path d=\"m16 9-5.5 5.5L8 12\" />\n</svg>\n"))
	})
	return svgCircleCheck
}

var onceCircleStop sync.Once

var svgCircleStop *ui.SVG

// CircleStop returns the Lucide circle stop icon.
func CircleStop() *ui.SVG {
	onceCircleStop.Do(func() {
		svgCircleStop = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <rect x=\"9\" y=\"9\" width=\"6\" height=\"6\" rx=\"1\" />\n</svg>\n"))
	})
	return svgCircleStop
}

var onceCircleX sync.Once

var svgCircleX *ui.SVG

// CircleX returns the Lucide circle x icon.
func CircleX() *ui.SVG {
	onceCircleX.Do(func() {
		svgCircleX = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <path d=\"m15 9-6 6\" />\n  <path d=\"m9 9 6 6\" />\n</svg>\n"))
	})
	return svgCircleX
}

var onceClipboardCheck sync.Once

var svgClipboardCheck *ui.SVG

// ClipboardCheck returns the Lucide clipboard check icon.
func ClipboardCheck() *ui.SVG {
	onceClipboardCheck.Do(func() {
		svgClipboardCheck = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"8\" height=\"4\" x=\"8\" y=\"2\" rx=\"1\" ry=\"1\" />\n  <path d=\"M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2\" />\n  <path d=\"m9 14 2 2 4-4\" />\n</svg>\n"))
	})
	return svgClipboardCheck
}

var onceClock sync.Once

var svgClock *ui.SVG

// Clock returns the Lucide clock icon.
func Clock() *ui.SVG {
	onceClock.Do(func() {
		svgClock = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <path d=\"M12 6v6l4 2\" />\n</svg>\n"))
	})
	return svgClock
}

var onceCloudOff sync.Once

var svgCloudOff *ui.SVG

// CloudOff returns the Lucide cloud off icon.
func CloudOff() *ui.SVG {
	onceCloudOff.Do(func() {
		svgCloudOff = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M10.94 5.274A7 7 0 0 1 15.71 10h1.79a4.5 4.5 0 0 1 4.222 6.057\" />\n  <path d=\"M18.796 18.81A4.5 4.5 0 0 1 17.5 19H9A7 7 0 0 1 5.79 5.78\" />\n  <path d=\"m2 2 20 20\" />\n</svg>\n"))
	})
	return svgCloudOff
}

var onceCopy sync.Once

var svgCopy *ui.SVG

// Copy returns the Lucide copy icon.
func Copy() *ui.SVG {
	onceCopy.Do(func() {
		svgCopy = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"14\" height=\"14\" x=\"8\" y=\"8\" rx=\"2\" ry=\"2\" />\n  <path d=\"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2\" />\n</svg>\n"))
	})
	return svgCopy
}

var onceCpu sync.Once

var svgCpu *ui.SVG

// Cpu returns the Lucide cpu icon.
func Cpu() *ui.SVG {
	onceCpu.Do(func() {
		svgCpu = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 20v2\" />\n  <path d=\"M12 2v2\" />\n  <path d=\"M17 20v2\" />\n  <path d=\"M17 2v2\" />\n  <path d=\"M2 12h2\" />\n  <path d=\"M2 17h2\" />\n  <path d=\"M2 7h2\" />\n  <path d=\"M20 12h2\" />\n  <path d=\"M20 17h2\" />\n  <path d=\"M20 7h2\" />\n  <path d=\"M7 20v2\" />\n  <path d=\"M7 2v2\" />\n  <rect x=\"4\" y=\"4\" width=\"16\" height=\"16\" rx=\"2\" />\n  <rect x=\"8\" y=\"8\" width=\"8\" height=\"8\" rx=\"1\" />\n</svg>\n"))
	})
	return svgCpu
}

var onceDownload sync.Once

var svgDownload *ui.SVG

// Download returns the Lucide download icon.
func Download() *ui.SVG {
	onceDownload.Do(func() {
		svgDownload = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 15V3\" />\n  <path d=\"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4\" />\n  <path d=\"m7 10 5 5 5-5\" />\n</svg>\n"))
	})
	return svgDownload
}

var onceEllipsis sync.Once

var svgEllipsis *ui.SVG

// Ellipsis returns the Lucide ellipsis icon.
func Ellipsis() *ui.SVG {
	onceEllipsis.Do(func() {
		svgEllipsis = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"1\" />\n  <circle cx=\"19\" cy=\"12\" r=\"1\" />\n  <circle cx=\"5\" cy=\"12\" r=\"1\" />\n</svg>\n"))
	})
	return svgEllipsis
}

var onceExternalLink sync.Once

var svgExternalLink *ui.SVG

// ExternalLink returns the Lucide external link icon.
func ExternalLink() *ui.SVG {
	onceExternalLink.Do(func() {
		svgExternalLink = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M15 3h6v6\" />\n  <path d=\"M10 14 21 3\" />\n  <path d=\"M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6\" />\n</svg>\n"))
	})
	return svgExternalLink
}

var onceFileText sync.Once

var svgFileText *ui.SVG

// FileText returns the Lucide file text icon.
func FileText() *ui.SVG {
	onceFileText.Do(func() {
		svgFileText = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M6 22a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.704.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2z\" />\n  <path d=\"M14 2v5a1 1 0 0 0 1 1h5\" />\n  <path d=\"M10 9H8\" />\n  <path d=\"M16 13H8\" />\n  <path d=\"M16 17H8\" />\n</svg>\n"))
	})
	return svgFileText
}

var onceFolder sync.Once

var svgFolder *ui.SVG

// Folder returns the Lucide folder icon.
func Folder() *ui.SVG {
	onceFolder.Do(func() {
		svgFolder = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z\" />\n</svg>\n"))
	})
	return svgFolder
}

var onceFolderOpen sync.Once

var svgFolderOpen *ui.SVG

// FolderOpen returns the Lucide folder open icon.
func FolderOpen() *ui.SVG {
	onceFolderOpen.Do(func() {
		svgFolderOpen = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2\" />\n</svg>\n"))
	})
	return svgFolderOpen
}

var onceFolderPlus sync.Once

var svgFolderPlus *ui.SVG

// FolderPlus returns the Lucide folder plus icon.
func FolderPlus() *ui.SVG {
	onceFolderPlus.Do(func() {
		svgFolderPlus = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 10v6\" />\n  <path d=\"M9 13h6\" />\n  <path d=\"M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z\" />\n</svg>\n"))
	})
	return svgFolderPlus
}

var onceGitBranch sync.Once

var svgGitBranch *ui.SVG

// GitBranch returns the Lucide git branch icon.
func GitBranch() *ui.SVG {
	onceGitBranch.Do(func() {
		svgGitBranch = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M15 6a9 9 0 0 0-9 9V3\" />\n  <circle cx=\"18\" cy=\"6\" r=\"3\" />\n  <circle cx=\"6\" cy=\"18\" r=\"3\" />\n</svg>\n"))
	})
	return svgGitBranch
}

var onceGitCompare sync.Once

var svgGitCompare *ui.SVG

// GitCompare returns the Lucide git compare icon.
func GitCompare() *ui.SVG {
	onceGitCompare.Do(func() {
		svgGitCompare = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"18\" cy=\"18\" r=\"3\" />\n  <circle cx=\"6\" cy=\"6\" r=\"3\" />\n  <path d=\"M13 6h3a2 2 0 0 1 2 2v7\" />\n  <path d=\"M11 18H8a2 2 0 0 1-2-2V9\" />\n</svg>\n"))
	})
	return svgGitCompare
}

var onceGitFork sync.Once

var svgGitFork *ui.SVG

// GitFork returns the Lucide git fork icon.
func GitFork() *ui.SVG {
	onceGitFork.Do(func() {
		svgGitFork = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"18\" r=\"3\" />\n  <circle cx=\"6\" cy=\"6\" r=\"3\" />\n  <circle cx=\"18\" cy=\"6\" r=\"3\" />\n  <path d=\"M18 9v2c0 .6-.4 1-1 1H7c-.6 0-1-.4-1-1V9\" />\n  <path d=\"M12 12v3\" />\n</svg>\n"))
	})
	return svgGitFork
}

var onceGitPullRequest sync.Once

var svgGitPullRequest *ui.SVG

// GitPullRequest returns the Lucide git pull request icon.
func GitPullRequest() *ui.SVG {
	onceGitPullRequest.Do(func() {
		svgGitPullRequest = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"18\" cy=\"18\" r=\"3\" />\n  <circle cx=\"6\" cy=\"6\" r=\"3\" />\n  <path d=\"M13 6h3a2 2 0 0 1 2 2v7\" />\n  <line x1=\"6\" x2=\"6\" y1=\"9\" y2=\"21\" />\n</svg>\n"))
	})
	return svgGitPullRequest
}

var onceGlobe sync.Once

var svgGlobe *ui.SVG

// Globe returns the Lucide globe icon.
func Globe() *ui.SVG {
	onceGlobe.Do(func() {
		svgGlobe = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <path d=\"M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20\" />\n  <path d=\"M2 12h20\" />\n</svg>\n"))
	})
	return svgGlobe
}

var onceInfo sync.Once

var svgInfo *ui.SVG

// Info returns the Lucide info icon.
func Info() *ui.SVG {
	onceInfo.Do(func() {
		svgInfo = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"12\" r=\"10\" />\n  <path d=\"M12 16v-4\" />\n  <path d=\"M12 8h.01\" />\n</svg>\n"))
	})
	return svgInfo
}

var onceKeyRound sync.Once

var svgKeyRound *ui.SVG

// KeyRound returns the Lucide key round icon.
func KeyRound() *ui.SVG {
	onceKeyRound.Do(func() {
		svgKeyRound = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z\" />\n  <circle cx=\"16.5\" cy=\"7.5\" r=\".5\" fill=\"currentColor\" />\n</svg>\n"))
	})
	return svgKeyRound
}

var onceKeyboard sync.Once

var svgKeyboard *ui.SVG

// Keyboard returns the Lucide keyboard icon.
func Keyboard() *ui.SVG {
	onceKeyboard.Do(func() {
		svgKeyboard = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M10 8h.01\" />\n  <path d=\"M12 12h.01\" />\n  <path d=\"M14 8h.01\" />\n  <path d=\"M16 12h.01\" />\n  <path d=\"M18 8h.01\" />\n  <path d=\"M6 8h.01\" />\n  <path d=\"M7 16h10\" />\n  <path d=\"M8 12h.01\" />\n  <rect width=\"20\" height=\"16\" x=\"2\" y=\"4\" rx=\"2\" />\n</svg>\n"))
	})
	return svgKeyboard
}

var onceLaptop sync.Once

var svgLaptop *ui.SVG

// Laptop returns the Lucide laptop icon.
func Laptop() *ui.SVG {
	onceLaptop.Do(func() {
		svgLaptop = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M18 5a2 2 0 0 1 2 2v8.526a2 2 0 0 0 .212.897l1.068 2.127a1 1 0 0 1-.9 1.45H3.62a1 1 0 0 1-.9-1.45l1.068-2.127A2 2 0 0 0 4 15.526V7a2 2 0 0 1 2-2z\" />\n  <path d=\"M20.054 15.987H3.946\" />\n</svg>\n"))
	})
	return svgLaptop
}

var onceLightbulb sync.Once

var svgLightbulb *ui.SVG

// Lightbulb returns the Lucide lightbulb icon.
func Lightbulb() *ui.SVG {
	onceLightbulb.Do(func() {
		svgLightbulb = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5\" />\n  <path d=\"M9 18h6\" />\n  <path d=\"M10 22h4\" />\n</svg>\n"))
	})
	return svgLightbulb
}

var onceLink sync.Once

var svgLink *ui.SVG

// Link returns the Lucide link icon.
func Link() *ui.SVG {
	onceLink.Do(func() {
		svgLink = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71\" />\n  <path d=\"M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71\" />\n</svg>\n"))
	})
	return svgLink
}

var onceListFilter sync.Once

var svgListFilter *ui.SVG

// ListFilter returns the Lucide list filter icon.
func ListFilter() *ui.SVG {
	onceListFilter.Do(func() {
		svgListFilter = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M2 5h20\" />\n  <path d=\"M6 12h12\" />\n  <path d=\"M9 19h6\" />\n</svg>\n"))
	})
	return svgListFilter
}

var onceLogIn sync.Once

var svgLogIn *ui.SVG

// LogIn returns the Lucide log in icon.
func LogIn() *ui.SVG {
	onceLogIn.Do(func() {
		svgLogIn = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m10 17 5-5-5-5\" />\n  <path d=\"M15 12H3\" />\n  <path d=\"M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4\" />\n</svg>\n"))
	})
	return svgLogIn
}

var onceMessageCircle sync.Once

var svgMessageCircle *ui.SVG

// MessageCircle returns the Lucide message circle icon.
func MessageCircle() *ui.SVG {
	onceMessageCircle.Do(func() {
		svgMessageCircle = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M2.992 16.342a2 2 0 0 1 .094 1.167l-1.065 3.29a1 1 0 0 0 1.236 1.168l3.413-.998a2 2 0 0 1 1.099.092 10 10 0 1 0-4.777-4.719\" />\n</svg>\n"))
	})
	return svgMessageCircle
}

var onceMessageCircleMore sync.Once

var svgMessageCircleMore *ui.SVG

// MessageCircleMore returns the Lucide message circle more icon.
func MessageCircleMore() *ui.SVG {
	onceMessageCircleMore.Do(func() {
		svgMessageCircleMore = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M2.992 16.342a2 2 0 0 1 .094 1.167l-1.065 3.29a1 1 0 0 0 1.236 1.168l3.413-.998a2 2 0 0 1 1.099.092 10 10 0 1 0-4.777-4.719\" />\n  <path d=\"M8 12h.01\" />\n  <path d=\"M12 12h.01\" />\n  <path d=\"M16 12h.01\" />\n</svg>\n"))
	})
	return svgMessageCircleMore
}

var onceMessageSquare sync.Once

var svgMessageSquare *ui.SVG

// MessageSquare returns the Lucide message square icon.
func MessageSquare() *ui.SVG {
	onceMessageSquare.Do(func() {
		svgMessageSquare = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z\" />\n</svg>\n"))
	})
	return svgMessageSquare
}

var onceMessageSquarePlus sync.Once

var svgMessageSquarePlus *ui.SVG

// MessageSquarePlus returns the Lucide message square plus icon.
func MessageSquarePlus() *ui.SVG {
	onceMessageSquarePlus.Do(func() {
		svgMessageSquarePlus = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z\" />\n  <path d=\"M12 8v6\" />\n  <path d=\"M9 11h6\" />\n</svg>\n"))
	})
	return svgMessageSquarePlus
}

var onceMonitor sync.Once

var svgMonitor *ui.SVG

// Monitor returns the Lucide monitor icon.
func Monitor() *ui.SVG {
	onceMonitor.Do(func() {
		svgMonitor = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"20\" height=\"14\" x=\"2\" y=\"3\" rx=\"2\" />\n  <line x1=\"8\" x2=\"16\" y1=\"21\" y2=\"21\" />\n  <line x1=\"12\" x2=\"12\" y1=\"17\" y2=\"21\" />\n</svg>\n"))
	})
	return svgMonitor
}

var onceMonitorPlay sync.Once

var svgMonitorPlay *ui.SVG

// MonitorPlay returns the Lucide monitor play icon.
func MonitorPlay() *ui.SVG {
	onceMonitorPlay.Do(func() {
		svgMonitorPlay = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M15.033 9.44a.647.647 0 0 1 0 1.12l-4.065 2.352a.645.645 0 0 1-.968-.56V7.648a.645.645 0 0 1 .967-.56z\" />\n  <path d=\"M12 17v4\" />\n  <path d=\"M8 21h8\" />\n  <rect x=\"2\" y=\"3\" width=\"20\" height=\"14\" rx=\"2\" />\n</svg>\n"))
	})
	return svgMonitorPlay
}

var onceMonitorSmartphone sync.Once

var svgMonitorSmartphone *ui.SVG

// MonitorSmartphone returns the Lucide monitor smartphone icon.
func MonitorSmartphone() *ui.SVG {
	onceMonitorSmartphone.Do(func() {
		svgMonitorSmartphone = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M18 8V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h8\" />\n  <path d=\"M10 19v-3.96 3.15\" />\n  <path d=\"M7 19h5\" />\n  <rect width=\"6\" height=\"10\" x=\"16\" y=\"12\" rx=\"2\" />\n</svg>\n"))
	})
	return svgMonitorSmartphone
}

var onceNetwork sync.Once

var svgNetwork *ui.SVG

// Network returns the Lucide network icon.
func Network() *ui.SVG {
	onceNetwork.Do(func() {
		svgNetwork = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect x=\"16\" y=\"16\" width=\"6\" height=\"6\" rx=\"1\" />\n  <rect x=\"2\" y=\"16\" width=\"6\" height=\"6\" rx=\"1\" />\n  <rect x=\"9\" y=\"2\" width=\"6\" height=\"6\" rx=\"1\" />\n  <path d=\"M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3\" />\n  <path d=\"M12 12V8\" />\n</svg>\n"))
	})
	return svgNetwork
}

var oncePanelLeft sync.Once

var svgPanelLeft *ui.SVG

// PanelLeft returns the Lucide panel left icon.
func PanelLeft() *ui.SVG {
	oncePanelLeft.Do(func() {
		svgPanelLeft = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\" />\n  <path d=\"M9 3v18\" />\n</svg>\n"))
	})
	return svgPanelLeft
}

var oncePanelRight sync.Once

var svgPanelRight *ui.SVG

// PanelRight returns the Lucide panel right icon.
func PanelRight() *ui.SVG {
	oncePanelRight.Do(func() {
		svgPanelRight = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"18\" height=\"18\" x=\"3\" y=\"3\" rx=\"2\" />\n  <path d=\"M15 3v18\" />\n</svg>\n"))
	})
	return svgPanelRight
}

var oncePaperclip sync.Once

var svgPaperclip *ui.SVG

// Paperclip returns the Lucide paperclip icon.
func Paperclip() *ui.SVG {
	oncePaperclip.Do(func() {
		svgPaperclip = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m16 6-8.414 8.586a2 2 0 0 0 2.829 2.829l8.414-8.586a4 4 0 1 0-5.657-5.657l-8.379 8.551a6 6 0 1 0 8.485 8.485l8.379-8.551\" />\n</svg>\n"))
	})
	return svgPaperclip
}

var oncePause sync.Once

var svgPause *ui.SVG

// Pause returns the Lucide pause icon.
func Pause() *ui.SVG {
	oncePause.Do(func() {
		svgPause = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect x=\"14\" y=\"3\" width=\"5\" height=\"18\" rx=\"1\" />\n  <rect x=\"5\" y=\"3\" width=\"5\" height=\"18\" rx=\"1\" />\n</svg>\n"))
	})
	return svgPause
}

var oncePencil sync.Once

var svgPencil *ui.SVG

// Pencil returns the Lucide pencil icon.
func Pencil() *ui.SVG {
	oncePencil.Do(func() {
		svgPencil = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z\" />\n  <path d=\"m15 5 4 4\" />\n</svg>\n"))
	})
	return svgPencil
}

var oncePin sync.Once

var svgPin *ui.SVG

// Pin returns the Lucide pin icon.
func Pin() *ui.SVG {
	oncePin.Do(func() {
		svgPin = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 17v5\" />\n  <path d=\"M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z\" />\n</svg>\n"))
	})
	return svgPin
}

var oncePlay sync.Once

var svgPlay *ui.SVG

// Play returns the Lucide play icon.
func Play() *ui.SVG {
	oncePlay.Do(func() {
		svgPlay = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z\" />\n</svg>\n"))
	})
	return svgPlay
}

var oncePlus sync.Once

var svgPlus *ui.SVG

// Plus returns the Lucide plus icon.
func Plus() *ui.SVG {
	oncePlus.Do(func() {
		svgPlus = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M5 12h14\" />\n  <path d=\"M12 5v14\" />\n</svg>\n"))
	})
	return svgPlus
}

var oncePower sync.Once

var svgPower *ui.SVG

// Power returns the Lucide power icon.
func Power() *ui.SVG {
	oncePower.Do(func() {
		svgPower = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 2v10\" />\n  <path d=\"M18.4 6.6a9 9 0 1 1-12.77.04\" />\n</svg>\n"))
	})
	return svgPower
}

var onceQrCode sync.Once

var svgQrCode *ui.SVG

// QrCode returns the Lucide qr code icon.
func QrCode() *ui.SVG {
	onceQrCode.Do(func() {
		svgQrCode = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"5\" height=\"5\" x=\"3\" y=\"3\" rx=\"1\" />\n  <rect width=\"5\" height=\"5\" x=\"16\" y=\"3\" rx=\"1\" />\n  <rect width=\"5\" height=\"5\" x=\"3\" y=\"16\" rx=\"1\" />\n  <path d=\"M21 16h-3a2 2 0 0 0-2 2v3\" />\n  <path d=\"M21 21v.01\" />\n  <path d=\"M12 7v3a2 2 0 0 1-2 2H7\" />\n  <path d=\"M3 12h.01\" />\n  <path d=\"M12 3h.01\" />\n  <path d=\"M12 16v.01\" />\n  <path d=\"M16 12h1\" />\n  <path d=\"M21 12v.01\" />\n  <path d=\"M12 21v-1\" />\n</svg>\n"))
	})
	return svgQrCode
}

var onceRefreshCw sync.Once

var svgRefreshCw *ui.SVG

// RefreshCw returns the Lucide refresh cw icon.
func RefreshCw() *ui.SVG {
	onceRefreshCw.Do(func() {
		svgRefreshCw = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8\" />\n  <path d=\"M21 3v5h-5\" />\n  <path d=\"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16\" />\n  <path d=\"M8 16H3v5\" />\n</svg>\n"))
	})
	return svgRefreshCw
}

var onceRotateCcw sync.Once

var svgRotateCcw *ui.SVG

// RotateCcw returns the Lucide rotate ccw icon.
func RotateCcw() *ui.SVG {
	onceRotateCcw.Do(func() {
		svgRotateCcw = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8\" />\n  <path d=\"M3 3v5h5\" />\n</svg>\n"))
	})
	return svgRotateCcw
}

var onceSearch sync.Once

var svgSearch *ui.SVG

// Search returns the Lucide search icon.
func Search() *ui.SVG {
	onceSearch.Do(func() {
		svgSearch = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"m21 21-4.34-4.34\" />\n  <circle cx=\"11\" cy=\"11\" r=\"8\" />\n</svg>\n"))
	})
	return svgSearch
}

var onceSend sync.Once

var svgSend *ui.SVG

// Send returns the Lucide send icon.
func Send() *ui.SVG {
	onceSend.Do(func() {
		svgSend = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z\" />\n  <path d=\"m21.854 2.147-10.94 10.939\" />\n</svg>\n"))
	})
	return svgSend
}

var onceSettings sync.Once

var svgSettings *ui.SVG

// Settings returns the Lucide settings icon.
func Settings() *ui.SVG {
	onceSettings.Do(func() {
		svgSettings = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915\" />\n  <circle cx=\"12\" cy=\"12\" r=\"3\" />\n</svg>\n"))
	})
	return svgSettings
}

var onceSlidersHorizontal sync.Once

var svgSlidersHorizontal *ui.SVG

// SlidersHorizontal returns the Lucide sliders horizontal icon.
func SlidersHorizontal() *ui.SVG {
	onceSlidersHorizontal.Do(func() {
		svgSlidersHorizontal = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M10 5H3\" />\n  <path d=\"M12 19H3\" />\n  <path d=\"M14 3v4\" />\n  <path d=\"M16 17v4\" />\n  <path d=\"M21 12h-9\" />\n  <path d=\"M21 19h-5\" />\n  <path d=\"M21 5h-7\" />\n  <path d=\"M8 10v4\" />\n  <path d=\"M8 12H3\" />\n</svg>\n"))
	})
	return svgSlidersHorizontal
}

var onceSmartphone sync.Once

var svgSmartphone *ui.SVG

// Smartphone returns the Lucide smartphone icon.
func Smartphone() *ui.SVG {
	onceSmartphone.Do(func() {
		svgSmartphone = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <rect width=\"14\" height=\"20\" x=\"5\" y=\"2\" rx=\"2\" ry=\"2\" />\n  <path d=\"M12 18h.01\" />\n</svg>\n"))
	})
	return svgSmartphone
}

var onceSparkles sync.Once

var svgSparkles *ui.SVG

// Sparkles returns the Lucide sparkles icon.
func Sparkles() *ui.SVG {
	onceSparkles.Do(func() {
		svgSparkles = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M11.017 2.814a1 1 0 0 1 1.966 0l1.051 5.558a2 2 0 0 0 1.594 1.594l5.558 1.051a1 1 0 0 1 0 1.966l-5.558 1.051a2 2 0 0 0-1.594 1.594l-1.051 5.558a1 1 0 0 1-1.966 0l-1.051-5.558a2 2 0 0 0-1.594-1.594l-5.558-1.051a1 1 0 0 1 0-1.966l5.558-1.051a2 2 0 0 0 1.594-1.594z\" />\n  <path d=\"M20 2v4\" />\n  <path d=\"M22 4h-4\" />\n  <circle cx=\"4\" cy=\"20\" r=\"2\" />\n</svg>\n"))
	})
	return svgSparkles
}

var onceSquarePen sync.Once

var svgSquarePen *ui.SVG

// SquarePen returns the Lucide square pen icon.
func SquarePen() *ui.SVG {
	onceSquarePen.Do(func() {
		svgSquarePen = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7\" />\n  <path d=\"M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z\" />\n</svg>\n"))
	})
	return svgSquarePen
}

var onceTerminal sync.Once

var svgTerminal *ui.SVG

// Terminal returns the Lucide terminal icon.
func Terminal() *ui.SVG {
	onceTerminal.Do(func() {
		svgTerminal = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M12 19h8\" />\n  <path d=\"m4 17 6-6-6-6\" />\n</svg>\n"))
	})
	return svgTerminal
}

var onceTrash sync.Once

var svgTrash *ui.SVG

// Trash returns the Lucide trash icon.
func Trash() *ui.SVG {
	onceTrash.Do(func() {
		svgTrash = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M10 11v6\" />\n  <path d=\"M14 11v6\" />\n  <path d=\"M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6\" />\n  <path d=\"M3 6h18\" />\n  <path d=\"M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2\" />\n</svg>\n"))
	})
	return svgTrash
}

var onceUndo sync.Once

var svgUndo *ui.SVG

// Undo returns the Lucide undo icon.
func Undo() *ui.SVG {
	onceUndo.Do(func() {
		svgUndo = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M3 7v6h6\" />\n  <path d=\"M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13\" />\n</svg>\n"))
	})
	return svgUndo
}

var onceUserRound sync.Once

var svgUserRound *ui.SVG

// UserRound returns the Lucide user round icon.
func UserRound() *ui.SVG {
	onceUserRound.Do(func() {
		svgUserRound = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <circle cx=\"12\" cy=\"8\" r=\"5\" />\n  <path d=\"M20 21a8 8 0 0 0-16 0\" />\n</svg>\n"))
	})
	return svgUserRound
}

var onceWrench sync.Once

var svgWrench *ui.SVG

// Wrench returns the Lucide wrench icon.
func Wrench() *ui.SVG {
	onceWrench.Do(func() {
		svgWrench = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.106-3.105c.32-.322.863-.22.983.218a6 6 0 0 1-8.259 7.057l-7.91 7.91a1 1 0 0 1-2.999-3l7.91-7.91a6 6 0 0 1 7.057-8.259c.438.12.54.662.219.984z\" />\n</svg>\n"))
	})
	return svgWrench
}

var onceX sync.Once

var svgX *ui.SVG

// X returns the Lucide x icon.
func X() *ui.SVG {
	onceX.Do(func() {
		svgX = ui.MustParseSVG([]byte("<svg\n  xmlns=\"http://www.w3.org/2000/svg\"\n  width=\"24\"\n  height=\"24\"\n  viewBox=\"0 0 24 24\"\n  fill=\"none\"\n  stroke=\"currentColor\"\n  stroke-width=\"2\"\n  stroke-linecap=\"round\"\n  stroke-linejoin=\"round\"\n>\n  <path d=\"M18 6 6 18\" />\n  <path d=\"m6 6 12 12\" />\n</svg>\n"))
	})
	return svgX
}
