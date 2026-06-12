#!/bin/zsh
# Generates app/Packaging/AppIcon.icns from the brand source image
# app/Packaging/Octo.png, fitted onto the native macOS icon grid: an 824×824
# squircle centered in a 1024 canvas (≈100 px margin) with a subtle contact
# shadow, so it sits naturally among other Dock/Launchpad icons.
# Re-run if Octo.png changes. Requires macOS (AppKit + sips + iconutil).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
master="$work/icon_1024.png"
source_img="$here/Octo.png"

[ -f "$source_img" ] || { echo "missing $source_img" >&2; exit 1; }

swift - "$master" "$source_img" <<'SWIFT'
import AppKit

let size: CGFloat = 1024
let outPath = CommandLine.arguments[1]
let srcPath = CommandLine.arguments[2]

guard let src = NSImage(contentsOfFile: srcPath) else {
    FileHandle.standardError.write(Data("cannot load source image\n".utf8)); exit(1)
}

let img = NSImage(size: NSSize(width: size, height: size))
img.lockFocus()
let ctx = NSGraphicsContext.current!
ctx.imageInterpolation = .high

// Apple icon grid: an 824×824 rounded-rect centered in the 1024 canvas, leaving
// a ~100 px transparent margin on each side. Corner radius ratio matches the
// system squircle (≈0.2247 of the side).
let inset: CGFloat = 100
let container = NSRect(x: inset, y: inset, width: size - 2*inset, height: size - 2*inset)
let radius = container.width * 0.2247
let squircle = NSBezierPath(roundedRect: container, xRadius: radius, yRadius: radius)

// Subtle contact shadow under the squircle (Finder/Launchpad render the icon on
// light backgrounds where the Dock's own shadow isn't applied).
NSGraphicsContext.saveGraphicsState()
let shadow = NSShadow()
shadow.shadowColor = NSColor.black.withAlphaComponent(0.20)
shadow.shadowOffset = NSSize(width: 0, height: -8)   // y-up canvas: negative = downward
shadow.shadowBlurRadius = 22
shadow.set()
NSColor.black.setFill()
squircle.fill()
NSGraphicsContext.restoreGraphicsState()

// Clip to the squircle and draw the brand image edge-to-edge inside it.
NSGraphicsContext.saveGraphicsState()
squircle.addClip()
src.draw(in: container, from: .zero, operation: .sourceOver, fraction: 1.0)
NSGraphicsContext.restoreGraphicsState()

img.unlockFocus()

guard let tiff = img.tiffRepresentation,
      let rep = NSBitmapImageRep(data: tiff),
      let png = rep.representation(using: .png, properties: [:]) else {
    FileHandle.standardError.write(Data("render failed\n".utf8)); exit(1)
}
try! png.write(to: URL(fileURLWithPath: outPath))
SWIFT

# Build the .iconset at all required sizes, then pack into .icns.
iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s "$master" --out "$iconset/icon_${s}x${s}.png" >/dev/null
  d=$((s*2)); sips -z $d $d "$master" --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$here/AppIcon.icns"
echo "wrote $here/AppIcon.icns"
rm -rf "$work"
