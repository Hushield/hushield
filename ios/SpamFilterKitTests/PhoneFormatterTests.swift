import XCTest
@testable import SpamFilterKit

final class PhoneFormatterTests: XCTestCase {

    // MARK: - formatAsTyped

    func test_formatAsTyped_buildsNANPProgressively() {
        // Every intermediate state has to look sane, because the user sees all
        // of them one keystroke at a time.
        XCTAssertEqual(PhoneFormatter.formatAsTyped(""), "")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("4"), "(4")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("42"), "(42")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("424"), "(424")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("4242"), "(424) 2")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("424237"), "(424) 237")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("4242374"), "(424) 237-4")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("4242374565"), "(424) 237-4565")
    }

    func test_formatAsTyped_isIdempotent() {
        // It runs on every keystroke against its own previous output, so
        // re-formatting formatted text must not corrupt it.
        let once = PhoneFormatter.formatAsTyped("4242374565")
        XCTAssertEqual(PhoneFormatter.formatAsTyped(once), once)
        XCTAssertEqual(PhoneFormatter.formatAsTyped(PhoneFormatter.formatAsTyped(once)), once)
    }

    func test_formatAsTyped_stripsPastedJunk() {
        XCTAssertEqual(PhoneFormatter.formatAsTyped("(424) 237-4565"), "(424) 237-4565")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("424.237.4565"), "(424) 237-4565")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("424 237 4565"), "(424) 237-4565")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("tel:424-237-4565"), "(424) 237-4565")
    }

    func test_formatAsTyped_treatsLeading1AsCountryCode() {
        XCTAssertEqual(PhoneFormatter.formatAsTyped("14242374565"), "+1 (424) 237-4565")
    }

    func test_formatAsTyped_preservesExplicitInternational() {
        // A leading + means the user has told us the country; do not reshape it.
        XCTAssertEqual(PhoneFormatter.formatAsTyped("+14242374565"), "+1 (424) 237-4565")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("+442071234567"), "+442071234567")
        XCTAssertEqual(PhoneFormatter.formatAsTyped("+81"), "+81")
    }

    // MARK: - normalize

    func test_normalize_acceptsEveryReasonableUSShape() {
        let want = "+14242374565"
        for input in [
            "4242374565",
            "424-237-4565",
            "(424) 237-4565",
            "424.237.4565",
            "1-424-237-4565",
            "14242374565",
            "+1 424 237 4565",
            "+1 (424) 237-4565",
            "  4242374565  ",
        ] {
            XCTAssertEqual(PhoneFormatter.normalize(input), want, "input: \(input)")
        }
    }

    func test_normalize_rejectsUnusableInput() {
        for input in ["", "   ", "abc", "555", "42423745", "12345678901234567890"] {
            XCTAssertNil(PhoneFormatter.normalize(input), "should reject: \(input)")
        }
    }

    // A NANP area code cannot begin with 0 or 1. Sending those to the server
    // guarantees a validation round-trip we can avoid locally.
    func test_normalize_rejectsInvalidNANPAreaCodes() {
        XCTAssertNil(PhoneFormatter.normalize("0242374565"))
        XCTAssertNil(PhoneFormatter.normalize("1242374565"))
        XCTAssertNil(PhoneFormatter.normalize("1-024-237-4565"))
    }

    func test_normalize_passesThroughExplicitInternational() {
        XCTAssertEqual(PhoneFormatter.normalize("+44 20 7123 4567"), "+442071234567")
        XCTAssertEqual(PhoneFormatter.normalize("+81 3 1234 5678"), "+81312345678")
        // Too short or absurdly long is still rejected.
        XCTAssertNil(PhoneFormatter.normalize("+1234"))
        XCTAssertNil(PhoneFormatter.normalize("+1234567890123456789"))
    }

    func test_normalize_outputIsAcceptedByPhoneNumberConversion() {
        // The normalized form has to survive the E.164 -> Int64 conversion the
        // Call Directory relies on, or a report can succeed while the number can
        // never be blocked.
        let e164 = PhoneFormatter.normalize("(424) 237-4565")
        XCTAssertEqual(e164, "+14242374565")
        XCTAssertEqual(PhoneNumber.e164ToInt64(e164!), 14242374565)
    }

    // MARK: - isValid / displayE164

    func test_isValid_matchesNormalize() {
        XCTAssertTrue(PhoneFormatter.isValid("(424) 237-4565"))
        XCTAssertFalse(PhoneFormatter.isValid("555"))
    }

    func test_displayE164() {
        XCTAssertEqual(PhoneFormatter.displayE164("+14242374565"), "+1 (424) 237-4565")
        // Non-NANP is returned untouched rather than forced into a US shape.
        XCTAssertEqual(PhoneFormatter.displayE164("+442071234567"), "+442071234567")
        XCTAssertEqual(PhoneFormatter.displayE164("garbage"), "garbage")
    }
}
