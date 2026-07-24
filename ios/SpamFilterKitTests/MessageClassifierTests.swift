import XCTest
@testable import SpamFilterKit

final class MessageClassifierTests: XCTestCase {

    // MARK: - blocked -> junk

    func test_blockedSender_classifiesAsJunk() {
        let state = BlocklistState(blocked: [14_155_551_111], labeled: [], names: [:], cursor: "c1")

        let action = MessageClassifier.classify(sender: "+14155551111", body: "hello", state: state)

        XCTAssertEqual(action, .junk)
    }

    // MARK: - labeled -> junk

    func test_labeledSender_classifiesAsJunk() {
        let state = BlocklistState(blocked: [], labeled: [14_155_552_222], names: [14_155_552_222: "Telemarketer"], cursor: "c1")

        let action = MessageClassifier.classify(sender: "+14155552222", body: "hi", state: state)

        XCTAssertEqual(action, .junk)
    }

    // MARK: - unknown -> allow

    func test_unknownSender_classifiesAsAllow() {
        let state = BlocklistState(blocked: [14_155_551_111], labeled: [14_155_552_222], names: [:], cursor: "c1")

        let action = MessageClassifier.classify(sender: "+14155559999", body: "hi", state: state)

        XCTAssertEqual(action, .allow)
    }

    // MARK: - empty state -> allow

    func test_emptyState_classifiesAsAllow() {
        let action = MessageClassifier.classify(sender: "+14155551111", body: "hi", state: BlocklistState())

        XCTAssertEqual(action, .allow)
    }

    // MARK: - unparseable sender -> allow (never crash, never wrongly junk)

    func test_unparseableSender_classifiesAsAllow() {
        let state = BlocklistState(blocked: [14_155_551_111], labeled: [], names: [:], cursor: "c1")

        XCTAssertEqual(MessageClassifier.classify(sender: "SHORTCODE", body: "hi", state: state), .allow)
        XCTAssertEqual(MessageClassifier.classify(sender: "12345", body: "hi", state: state), .allow)
        XCTAssertEqual(MessageClassifier.classify(sender: "", body: "hi", state: state), .allow)
    }

    // MARK: - body content never affects the decision (sender-only classifier)

    func test_bodyContent_doesNotAffectDecision() {
        let state = BlocklistState(blocked: [14_155_551_111], labeled: [], names: [:], cursor: "c1")

        let junkBody = MessageClassifier.classify(sender: "+14155551111", body: "URGENT click here now!!!", state: state)
        let plainBody = MessageClassifier.classify(sender: "+14155551111", body: "hey, running late", state: state)

        XCTAssertEqual(junkBody, .junk)
        XCTAssertEqual(plainBody, .junk)
    }
}
