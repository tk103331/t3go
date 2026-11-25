// Renders the macOS icon from the square app icon: the white outside the
// rounded square becomes transparent, and the artwork sits in the 824px
// content square of a 1024px canvas, as macOS's icon grid expects.
//
//   swift scripts/make-macos-icon.swift
import AppKit
import Foundation

let srcPath = "cmd/t3go-app/assets/Icon.png"
let dstPath = "cmd/t3go-app/assets/Icon-macos.png"

let src = NSBitmapImageRep(data: try Data(contentsOf: URL(fileURLWithPath: srcPath)))!
let w = src.pixelsWide, h = src.pixelsHigh

// The white outside the rounded square becomes transparent; the light
// gray "go" stays opaque, so only near-white pixels fade.
let cleaned = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: w, pixelsHigh: h,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
for y in 0..<h {
    for x in 0..<w {
        let c = src.colorAt(x: x, y: y)!.usingColorSpace(.deviceRGB)!
        let m = min(c.redComponent, c.greenComponent, c.blueComponent)
        let a = max(0, min(1, (0.97 - Double(m)) / 0.08))
        cleaned.setColor(NSColor(deviceRed: c.redComponent, green: c.greenComponent,
            blue: c.blueComponent, alpha: CGFloat(a)), atX: x, y: y)
    }
}

let out = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1024, pixelsHigh: 1024,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
out.size = NSSize(width: 1024, height: 1024)
let ctx = NSGraphicsContext(bitmapImageRep: out)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = ctx
ctx.cgContext.clear(CGRect(x: 0, y: 0, width: 1024, height: 1024))
let image = NSImage(size: NSSize(width: w, height: h))
image.addRepresentation(cleaned)
image.draw(in: NSRect(x: 100, y: 100, width: 824, height: 824), from: .zero,
    operation: .sourceOver, fraction: 1, respectFlipped: false,
    hints: [.interpolation: NSImageInterpolation.high])
NSGraphicsContext.restoreGraphicsState()
try out.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: dstPath))
print("wrote \(dstPath)")
