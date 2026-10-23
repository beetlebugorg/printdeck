// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import UserNotifications

/// Decrypts the message of the deck in the key "e" of a push (relay/README.md), and shows its title
/// and body. When it cannot, iOS shows the alert of the push: "Printdeck: A message from your
/// printer". The watch shows the notification of the iPhone.
final class NotificationService: UNNotificationServiceExtension {
    private var handler: ((UNNotificationContent) -> Void)?
    private var content: UNMutableNotificationContent?

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
        contentHandler(content)
        handler = nil
    }

    override func serviceExtensionTimeWillExpire() {
        if let handler, let content {
            handler(content)
        }
        handler = nil
    }
}
