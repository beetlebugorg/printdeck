// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import AVFoundation
import CoreVideo
import Foundation
import ImageIO

/// The time lapse videos on the iPhone. The deck writes a QuickTime file of JPEG frames (Motion
/// JPEG). The app downloads it to its caches folder, and converts it to H.264 there, frame by
/// frame: ImageIO decodes each JPEG, and AVAssetWriter encodes the video. The player, Share and
/// Save to Photos use the H.264 file. The conversion runs on the iPhone; the deck only sends the
/// file. iOS removes the caches folder when it needs the space.
enum Timelapse {
    /// The folder of the videos in the caches folder.
    static var folder: URL {
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appending(path: "Timelapse", directoryHint: .isDirectory)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    /// The H.264 file of a video of the deck.
    static func playable(_ v: DeckVideo) -> URL {
        folder.appending(path: (v.name as NSString).deletingPathExtension + ".mp4")
    }

    /// Removes the files of the videos that the deck does not have now.
    static func prune(keep videos: [DeckVideo]) {
        let names = Set(videos.map { playable($0).lastPathComponent })
        let files = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)) ?? []
        for f in files where !names.contains(f.lastPathComponent) {
            try? FileManager.default.removeItem(at: f)
        }
    }

    /// Returns the H.264 file of a video: from the cache, or after the download and the
    /// conversion.
    static func prepare(_ v: DeckVideo, client: DeckClient) async throws -> URL {
        let out = playable(v)
        if FileManager.default.fileExists(atPath: out.path) { return out }
        let mov = folder.appending(path: v.name)
        try await client.download(v, to: mov)
        defer { try? FileManager.default.removeItem(at: mov) }
        let tmp = folder.appending(path: ".converting-" + out.lastPathComponent)
        try? FileManager.default.removeItem(at: tmp)
        try await convert(mov, to: tmp)
        try FileManager.default.moveItem(at: tmp, to: out)
        return out
    }

    /// Converts a QuickTime file of JPEG frames to an H.264 file (.mp4) at the same size and rate.
    static func convert(_ src: URL, to dst: URL) async throws {
        let movie = try MovieIndex(url: src)
        let fh = try FileHandle(forReadingFrom: src)
        defer { try? fh.close() }
        // H.264 needs an even size.
        let w = movie.width & ~1, h = movie.height & ~1
        let writer = try AVAssetWriter(outputURL: dst, fileType: .mp4)
        let input = AVAssetWriterInput(mediaType: .video, outputSettings: [
            AVVideoCodecKey: AVVideoCodecType.h264,
            AVVideoWidthKey: w,
            AVVideoHeightKey: h,
            AVVideoCompressionPropertiesKey: [AVVideoAverageBitRateKey: w * h * 6],
        ])
        input.expectsMediaDataInRealTime = false
        let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
            kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
            kCVPixelBufferWidthKey as String: w,
            kCVPixelBufferHeightKey as String: h,
        ])
        writer.add(input)
        guard writer.startWriting() else { throw writer.error ?? MovieError.write }
        writer.startSession(atSourceTime: .zero)
        for i in 0..<movie.count {
            try Task.checkCancellation()
            try fh.seek(toOffset: UInt64(movie.offsets[i]))
            guard let data = try fh.read(upToCount: movie.sizes[i]),
                  let src = CGImageSourceCreateWithData(data as CFData, nil),
                  let img = CGImageSourceCreateImageAtIndex(src, 0, nil) else { throw MovieError.frame(i) }
            while !input.isReadyForMoreMediaData {
                try await Task.sleep(for: .milliseconds(5))
            }
            guard let pool = adaptor.pixelBufferPool else { throw writer.error ?? MovieError.write }
            var buf: CVPixelBuffer?
            CVPixelBufferPoolCreatePixelBuffer(nil, pool, &buf)
            guard let buf, draw(img, into: buf),
                  adaptor.append(buf, withPresentationTime: CMTime(value: CMTimeValue(i), timescale: CMTimeScale(movie.fps)))
            else { throw writer.error ?? MovieError.write }
        }
        input.markAsFinished()
        await writer.finishWriting()
        if writer.status != .completed { throw writer.error ?? MovieError.write }
    }

    /// Draws the picture over the whole pixel buffer.
    private static func draw(_ img: CGImage, into buf: CVPixelBuffer) -> Bool {
        CVPixelBufferLockBaseAddress(buf, [])
        defer { CVPixelBufferUnlockBaseAddress(buf, []) }
        let w = CVPixelBufferGetWidth(buf), h = CVPixelBufferGetHeight(buf)
        guard let ctx = CGContext(data: CVPixelBufferGetBaseAddress(buf), width: w, height: h, bitsPerComponent: 8,
                                  bytesPerRow: CVPixelBufferGetBytesPerRow(buf), space: CGColorSpaceCreateDeviceRGB(),
                                  bitmapInfo: CGImageAlphaInfo.noneSkipFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue)
        else { return false }
        ctx.draw(img, in: CGRect(x: 0, y: 0, width: w, height: h))
        return true
    }
}

enum MovieError: LocalizedError {
    case format(String)
    case frame(Int)
    case write

    var errorDescription: String? {
        switch self {
        case .format(let what): "The video has a bad \(what)."
        case .frame(let i): "Frame \(i + 1) of the video cannot be read."
        case .write: "The video cannot be converted."
        }
    }
}

/// The index of a QuickTime file of JPEG frames: the size, the rate, and the place of each frame
/// in the file (the sample table). The deck writes these files (deck/internal/timelapse/mov.go).
struct MovieIndex {
    var width = 0, height = 0, fps = 0
    var offsets: [Int64] = []
    var sizes: [Int] = []
    var count: Int { offsets.count }

    init(url: URL) throws {
        let fh = try FileHandle(forReadingFrom: url)
        defer { try? fh.close() }
        let end = try fh.seekToEnd()
        // The atoms at the top level: find moov.
        var at: UInt64 = 0
        var moov: Data?
        while at + 8 <= end {
            try fh.seek(toOffset: at)
            guard let h = try fh.read(upToCount: 16), h.count >= 8 else { break }
            var size = UInt64(h.be32(0)), head: UInt64 = 8
            if size == 1, h.count >= 16 { size = h.be64(8); head = 16 }
            if size == 0 { size = end - at }
            guard size >= head, at + size <= end else { throw MovieError.format("atom") }
            if h.fourCC(4) == "moov" {
                guard size - head <= 8 << 20 else { throw MovieError.format("index") }
                try fh.seek(toOffset: at + head)
                moov = try fh.read(upToCount: Int(size - head))
                break
            }
            at += size
        }
        guard let moov, let trak = Self.videoTrack(moov) else { throw MovieError.format("index") }
        try parse(trak, fileSize: end)
    }

    /// The children of an atom: the type and the data of each.
    static func children(_ d: Data) -> [(String, Data)] {
        var out: [(String, Data)] = []
        var i = d.startIndex
        while i + 8 <= d.endIndex {
            let size = Int(d.be32(i - d.startIndex))
            guard size >= 8, i + size <= d.endIndex else { break }
            out.append((d.fourCC(i - d.startIndex + 4), d.subdata(in: i + 8..<i + size)))
            i += size
        }
        return out
    }

    static func child(_ d: Data, _ path: String...) -> Data? {
        var cur = d
        for kind in path {
            guard let next = children(cur).first(where: { $0.0 == kind })?.1 else { return nil }
            cur = next
        }
        return cur
    }

    /// The first track with JPEG frames.
    static func videoTrack(_ moov: Data) -> Data? {
        children(moov).first { kind, trak in
            guard kind == "trak", let stsd = child(trak, "mdia", "minf", "stbl", "stsd"), stsd.count >= 16 else { return false }
            let codec = stsd.fourCC(12)
            return codec == "jpeg" || codec == "mjpa"
        }?.1
    }

    private mutating func parse(_ trak: Data, fileSize: UInt64) throws {
        guard let mdhd = Self.child(trak, "mdia", "mdhd"), mdhd.count >= 24,
              let stbl = Self.child(trak, "mdia", "minf", "stbl"),
              let stsd = Self.child(stbl, "stsd"), stsd.count >= 16 + 28,
              let stts = Self.child(stbl, "stts"), stts.count >= 16,
              let stsc = Self.child(stbl, "stsc"), stsc.count >= 8,
              let stsz = Self.child(stbl, "stsz"), stsz.count >= 12 else { throw MovieError.format("index") }
        let co64 = Self.child(stbl, "co64")
        guard let stco = co64 ?? Self.child(stbl, "stco"), stco.count >= 8 else { throw MovieError.format("index") }
        let scale = mdhd.be32(12), delta = stts.be32(12)
        width = Int(stsd.be16(16 + 24))
        height = Int(stsd.be16(16 + 26))
        fps = delta > 0 ? Int((scale + delta / 2) / delta) : 0
        guard width > 0, height > 0, fps > 0 else { throw MovieError.format("size or rate") }

        let n = Int(stsz.be32(8))
        let same = Int(stsz.be32(4))
        guard n > 0, same > 0 || stsz.count >= 12 + 4 * n else { throw MovieError.format("index") }
        sizes = (0..<n).map { same > 0 ? same : Int(stsz.be32(12 + 4 * $0)) }

        let chunks = Int(stco.be32(4))
        let width64 = co64 != nil
        guard chunks > 0, stco.count >= 8 + (width64 ? 8 : 4) * chunks else { throw MovieError.format("index") }
        func chunkAt(_ c: Int) -> Int64 { width64 ? Int64(stco.be64(8 + 8 * c)) : Int64(stco.be32(8 + 4 * c)) }
        let runs = Int(stsc.be32(4))
        guard runs > 0, stsc.count >= 8 + 12 * runs else { throw MovieError.format("index") }
        for r in 0..<runs where offsets.count < n {
            let first = Int(stsc.be32(8 + 12 * r)) - 1
            let per = Int(stsc.be32(8 + 12 * r + 4))
            let last = r + 1 < runs ? Int(stsc.be32(8 + 12 * (r + 1))) - 1 : chunks
            guard first >= 0, last <= chunks, per > 0 else { throw MovieError.format("index") }
            for c in first..<last where offsets.count < n {
                var at = chunkAt(c)
                for _ in 0..<per where offsets.count < n {
                    let size = sizes[offsets.count]
                    guard at + Int64(size) <= Int64(fileSize) else { throw MovieError.format("index") }
                    offsets.append(at)
                    at += Int64(size)
                }
            }
        }
        guard offsets.count == n else { throw MovieError.format("index") }
    }
}

private extension Data {
    func byte(_ i: Int) -> UInt8 { self[startIndex + i] }
    func be16(_ i: Int) -> UInt16 { UInt16(byte(i)) << 8 | UInt16(byte(i + 1)) }
    func be32(_ i: Int) -> UInt32 { UInt32(be16(i)) << 16 | UInt32(be16(i + 2)) }
    func be64(_ i: Int) -> UInt64 { UInt64(be32(i)) << 32 | UInt64(be32(i + 4)) }
    func fourCC(_ i: Int) -> String { String(decoding: self[(startIndex + i)..<(startIndex + i + 4)], as: UTF8.self) }
}
