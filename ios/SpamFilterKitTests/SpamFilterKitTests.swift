import XCTest
@testable import SpamFilterKit

final class SpamFilterKitTests: XCTestCase {
    func testVersionIsSet() {
        XCTAssertEqual(SpamFilterKit.version, "0.1.0")
    }
}
