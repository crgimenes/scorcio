// Scorcio icon: a door left ajar, seen in foreshortening (scorcio), a glimpse
// of warm light through it, on a dark squircle. Renders a 1024x1024 PNG:
//   swift assets/icon.swift icon.png  (then iconutil builds assets/scorcio.icns)
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let S: CGFloat = 1024
let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "icon.png"
let cs = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: Int(S), height: Int(S), bitsPerComponent: 8, bytesPerRow: 0,
                    space: cs, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!

func rgb(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255,
            blue: CGFloat(hex & 0xff) / 255, alpha: a)
}
func gradient(_ stops: [(UInt32, CGFloat)]) -> CGGradient {
    CGGradient(colorsSpace: cs, colors: stops.map { rgb($0.0) } as CFArray, locations: stops.map { $0.1 })!
}
// y grows up in CoreGraphics; describe the art top-down and flip here.
func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: x, y: S - y) }

// Squircle body on Apple's 1024 grid (824 body, ~185 corner radius).
let inset: CGFloat = 100
let body = CGRect(x: inset, y: inset, width: S - 2 * inset, height: S - 2 * inset)
let squircle = CGPath(roundedRect: body, cornerWidth: 185, cornerHeight: 185, transform: nil)
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -12), blur: 28, color: rgb(0x000000, 0.45))
ctx.addPath(squircle); ctx.setFillColor(rgb(0x14161f)); ctx.fillPath()
ctx.restoreGState()
ctx.saveGState()
ctx.addPath(squircle); ctx.clip()
ctx.drawLinearGradient(gradient([(0x22263a, 0), (0x0c0d14, 1)]), start: p(512, 100), end: p(512, 924), options: [])

// A door left ajar, seen in foreshortening (scorcio): through the gap between
// its far edge and the frame, a glimpse of warm evening light.
let frameL: CGFloat = 330, frameR: CGFloat = 694, frameT: CGFloat = 214, frameB: CGFloat = 806
let opening = CGRect(x: frameL, y: S - frameB, width: frameR - frameL, height: frameB - frameT)

// Light spilling out onto the floor from the gap.
ctx.saveGState()
let beam = CGMutablePath()
beam.move(to: p(574, frameB)); beam.addLine(to: p(frameR, frameB))
beam.addLine(to: p(860, 930)); beam.addLine(to: p(600, 930)); beam.closeSubpath()
ctx.addPath(beam); ctx.clip()
ctx.setBlendMode(.screen)
ctx.drawLinearGradient(CGGradient(colorsSpace: cs, colors: [rgb(0xffd98a, 0.85), rgb(0xffb347, 0.35), rgb(0xffb347, 0)] as CFArray,
                                  locations: [0, 0.45, 1])!, start: p(640, frameB), end: p(720, 930), options: [])
ctx.restoreGState()

// Glow around the opening.
ctx.saveGState()
ctx.setShadow(offset: .zero, blur: 80, color: rgb(0xffb347, 0.5))
ctx.setFillColor(rgb(0xffb347)); ctx.fill(opening)
ctx.restoreGState()

// The view beyond the door: amber sky, a low sun, a dark horizon.
ctx.saveGState()
ctx.clip(to: opening)
ctx.drawLinearGradient(gradient([(0xffe39a, 0), (0xffb347, 0.5), (0xff7a3d, 1)]),
                       start: p(512, frameT), end: p(512, 600), options: [.drawsAfterEndLocation])
ctx.setFillColor(rgb(0xfff3c4)); ctx.fillEllipse(in: CGRect(x: 600, y: S - 612, width: 110, height: 110))
ctx.setFillColor(rgb(0x2a1b12)); ctx.fill(CGRect(x: frameL, y: S - frameB, width: frameR - frameL, height: frameB - 592))
ctx.restoreGState()

// The door leaf, hinged on the left and swung in: its free edge is farther
// away, so shorter.
let leaf = CGMutablePath()
leaf.move(to: p(frameL, frameT)); leaf.addLine(to: p(574, frameT + 34))
leaf.addLine(to: p(574, frameB - 34)); leaf.addLine(to: p(frameL, frameB)); leaf.closeSubpath()
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 10, height: 0), blur: 24, color: rgb(0x000000, 0.6))
ctx.addPath(leaf); ctx.setFillColor(rgb(0x1a1d2b)); ctx.fillPath()
ctx.restoreGState()
ctx.saveGState()
ctx.addPath(leaf); ctx.clip()
ctx.drawLinearGradient(gradient([(0x262a3d, 0), (0x161824, 1)]), start: p(frameL, 500), end: p(574, 500), options: [])
ctx.restoreGState()
// Its lit edge, catching the light.
ctx.setStrokeColor(rgb(0xffcf7a, 0.9)); ctx.setLineWidth(6); ctx.setLineCap(.round)
ctx.move(to: p(574, frameT + 34)); ctx.addLine(to: p(574, frameB - 34)); ctx.strokePath()

// The frame.
ctx.setStrokeColor(rgb(0xf6e7c4)); ctx.setLineWidth(16); ctx.setLineJoin(.round)
ctx.stroke(opening)
ctx.restoreGState()

// A hairline edge so the squircle reads on dark docks too.
ctx.addPath(squircle); ctx.setStrokeColor(rgb(0xffffff, 0.08)); ctx.setLineWidth(3); ctx.strokePath()

let img = ctx.makeImage()!
let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: out) as CFURL, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, img, nil)
CGImageDestinationFinalize(dest)
print("wrote", out)
