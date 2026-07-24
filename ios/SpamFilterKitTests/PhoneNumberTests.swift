import XCTest
@testable import SpamFilterKit

final class PhoneNumberTests: XCTestCase {

    // MARK: - Round-trips (valid inputs)

    func test_roundTrip_11DigitNANP() throws {
        let e164 = "+14155551234"
        let value = PhoneNumber.e164ToInt64(e164)
        XCTAssertEqual(value, 14_155_551_234)
        XCTAssertEqual(PhoneNumber.int64ToE164(try XCTUnwrap(value)), e164)
    }

    func test_roundTrip_longerInternational() throws {
        // +44 (UK) with a longer subscriber number -- 13 digits after '+'.
        let e164 = "+442071838750"
        let value = PhoneNumber.e164ToInt64(e164)
        XCTAssertEqual(value, 442_071_838_750)
        XCTAssertEqual(PhoneNumber.int64ToE164(try XCTUnwrap(value)), e164)
    }

    func test_roundTrip_minimumLength_8Digits() throws {
        let e164 = "+12345678"
        let value = PhoneNumber.e164ToInt64(e164)
        XCTAssertEqual(value, 12_345_678)
        XCTAssertEqual(PhoneNumber.int64ToE164(try XCTUnwrap(value)), e164)
    }

    func test_roundTrip_maximumLength_15Digits() throws {
        let e164 = "+123456789012345"
        let value = PhoneNumber.e164ToInt64(e164)
        XCTAssertEqual(value, 123_456_789_012_345)
        XCTAssertEqual(PhoneNumber.int64ToE164(try XCTUnwrap(value)), e164)
    }

    // MARK: - Invalid inputs -> nil

    func test_empty_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64(""))
    }

    func test_missingPlus_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("14155551234"))
    }

    func test_nonDigits_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("+1415555abcd"))
    }

    func test_embeddedSpaces_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("+1 415 555 1234"))
    }

    func test_tooLong_16Digits_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("+1234567890123456"))
    }

    func test_tooShort_7Digits_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("+1234567"))
    }

    func test_justAPlus_missingDigits_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("+"))
    }

    func test_doublePlus_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("++14155551234"))
    }

    func test_plusInMiddle_isInvalid() {
        XCTAssertNil(PhoneNumber.e164ToInt64("1415+5551234"))
    }

    func test_leadingZeroAfterPlus_isInvalid() {
        // E.164 numbers never have a leading zero after '+'; rejecting these
        // also protects the round-trip guarantee (Int64 would otherwise
        // silently drop the zero).
        XCTAssertNil(PhoneNumber.e164ToInt64("+01234567"))
    }

    // MARK: - int64ToE164 (direct)

    func test_int64ToE164_prependsPlus() {
        XCTAssertEqual(PhoneNumber.int64ToE164(14_155_551_234), "+14155551234")
    }
}
