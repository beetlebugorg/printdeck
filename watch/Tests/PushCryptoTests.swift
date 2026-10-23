import CryptoKit
import XCTest

/// Vectors from the Pair and Seal of the deck (deck/internal/notify/phones.go), with a test key
/// pair and a test device token.
final class PushCryptoTests: XCTestCase {
    let appPrivate = "3ZA0kOJF0k0NSchsj9Qj2Vk5HhSfha5cErGNlKQnds0="
    let deckPublic = "G4n/j7ulE+5r0cLgj7gQQAFmFRTlYQUsnIcmn7+HTiE="
    let device = "6e52ca2dd6c49f4bc59297349ebbcb06d12e2d58b3d139d557208fad26dd41a4"
    let key = "sSCcXcZlIg037s2KWyvuAlkNUuf1gtvRA+7AVbEFkHA="
    let payload = "xPr7xuy5k72GUnOlZHdit0T9e1TmxDaxfEKVDu8YwhtO1ZH+ku7/IoxOsz7gck5GJVIwe6XXwAWqicxHpbbvrOoEzrg4xusP0MoGYBwmNXAneZG4o+Y8E4tYd/rzj3Z/M6G6nQCjWKYUOVqeDGc0+6TetrneyD14BEIjDA=="

    func testMessageKey() throws {
        let priv = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: Data(base64Encoded: appPrivate)!)
        let k = try PushCrypto.messageKey(privateKey: priv, deckPublicKey: Data(base64Encoded: deckPublic)!,
                                          deviceToken: Data(hex: device)!)
        XCTAssertEqual(k, Data(base64Encoded: key)!)
    }

    func testOpen() throws {
        let m = try PushCrypto.open(payload, key: Data(base64Encoded: key)!)
        XCTAssertEqual(m, PushCrypto.Message(title: "Print done", body: "benchy.gcode · 1h 42m",
                                             tags: "white_check_mark", at: 1792790000))
    }

    func testWrongKey() {
        var k = Data(base64Encoded: key)!
        k[0] ^= 1
        XCTAssertThrowsError(try PushCrypto.open(payload, key: k))
    }

    func testBadPayload() {
        XCTAssertThrowsError(try PushCrypto.open("not base64", key: Data(base64Encoded: key)!))
        XCTAssertThrowsError(try PushCrypto.open(String(payload.dropLast(8)) + "AAAAAAA=", key: Data(base64Encoded: key)!))
    }

    func testHex() {
        XCTAssertEqual(Data(hex: device)?.hex, device)
        XCTAssertNil(Data(hex: "abc"))
        XCTAssertNil(Data(hex: "zz"))
    }
}
