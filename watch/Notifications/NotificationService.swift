// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation
import UserNotifications

/// Decrypts the message of the deck in the key "e" of a push (relay/README.md), and shows its title
/// and body. When it cannot, iOS shows the alert of the push: "Printdeck: A message from your
/// printer". The watch shows the notification of the iPhone.
///
/// When the iPhone reaches the deck and the deck has a camera on, the notification gets a picture
/// of the picture camera of the deck (the main camera on an older deck): the part when the camera
/// has a calibration and the deck has a print, else the whole picture. Away from the network of
/// the deck, the notification comes with no picture.
final class NotificationService: UNNotificationServiceExtension {
    private var handler: ((UNNotificationContent) -> Void)?
    private var content: UNMutableNotificationContent?
    /// The work and the end of the time can both call finish.
    private let lock = NSLock()

    override func didReceive(_ request: UNNotificationRequest,
                             withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        handler = contentHandler
        guard let content = request.content.mutableCopy() as? UNMutableNotificationContent else {
            contentHandler(request.content)
            return
        }
        self.content = content
        if let e = content.userInfo["e"] as? String, let p = Store.pairing,
           let m = try? PushCrypto.open(e, key: p.key) {
            content.title = m.title
            content.body = m.body
            content.threadIdentifier = "printdeck"
        }
        Task {
            if let a = await Self.cameraPicture() { content.attachments = [a] }
            finish()
        }
    }

    /// Sends the content once: at the end of the work, or when the time is up.
    private func finish() {
        lock.lock()
        let h = handler
        handler = nil
        lock.unlock()
        if let h, let content { h(content) }
    }

    /// A picture of the camera of the deck as an attachment, or nil. The reads of the deck stop
    /// after 8 seconds each, inside the 30 seconds of the extension.
    private static func cameraPicture() async -> UNNotificationAttachment? {
        let client = DeckClient(settings: Store.settings)
        guard let cam = await client.pictureCamera(),
              let jpeg = await client.snapshotJPEG(width: 1080, view: "part", camera: cam.id) else { return nil }
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString + ".jpg")
        guard (try? jpeg.write(to: url)) != nil else { return nil }
        return try? UNNotificationAttachment(identifier: "camera", url: url,
                                             options: [UNNotificationAttachmentOptionsTypeHintKey: "public.jpeg"])
    }

    override func serviceExtensionTimeWillExpire() {
        finish()
    }
}
