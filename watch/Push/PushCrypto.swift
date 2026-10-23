import CryptoKit
import Foundation

/// The encryption of the push messages of the deck (relay/README.md). The app and the deck agree on
/// a message key with X25519 and HKDF-SHA256. The deck encrypts each message with AES-256-GCM.
enum PushCrypto {
    /// The info of the HKDF. The salt is the device token.
    static let info = Data("printdeck push v1".utf8)

    /// A message of the deck.
    struct Message: Decodable, Equatable {
        var title: String
        var body: String
        var tags: String?
        /// The time of the event, in Unix seconds.
        var at: Int64
    }

    enum Failure: Error {
        case badKey
        case badPayload
    }

    /// The message key from the private key of the app, the public key of the deck (raw, 32 bytes)
    /// and the device token.
    static func messageKey(privateKey: Curve25519.KeyAgreement.PrivateKey, deckPublicKey: Data,
                           deviceToken: Data) throws -> Data {
        guard let pub = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: deckPublicKey) else {
            throw Failure.badKey
        }
        let shared = try privateKey.sharedSecretFromKeyAgreement(with: pub)
        let key = shared.hkdfDerivedSymmetricKey(using: SHA256.self, salt: deviceToken, sharedInfo: info,
                                                 outputByteCount: 32)
        return key.withUnsafeBytes { Data($0) }
    }

    /// Decrypts a payload: base64 of a 12-byte nonce, the ciphertext and the 16-byte tag.
    static func open(_ payload: String, key: Data) throws -> Message {
        guard key.count == 32, let data = Data(base64Encoded: payload),
              let box = try? AES.GCM.SealedBox(combined: data) else {
            throw Failure.badPayload
        }
        let plain = try AES.GCM.open(box, using: SymmetricKey(data: key))
        return try JSONDecoder().decode(Message.self, from: plain)
    }
}

extension Data {
    /// The bytes as lowercase hex.
    var hex: String { map { String(format: "%02x", $0) }.joined() }

    /// The bytes of a hex string, or nil.
    init?(hex: String) {
        guard hex.count % 2 == 0 else { return nil }
        var out = Data(capacity: hex.count / 2)
        var i = hex.startIndex
        while i < hex.endIndex {
            let j = hex.index(i, offsetBy: 2)
            guard let b = UInt8(hex[i..<j], radix: 16) else { return nil }
            out.append(b)
            i = j
        }
        self = out
    }
}
